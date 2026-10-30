package tlsid

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/cometbft/cometbft/crypto"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
)

const fuzzOtherChainID = vectorChainID + "-other"

// loadFuzzVectors returns the committed golden vectors used as fuzz seeds and
// the verifier keys they reference.
func loadFuzzVectors(f *testing.F) ([]vectorCase, []crypto.PubKey) {
	f.Helper()
	data, err := os.ReadFile(vectorsPath)
	if err != nil {
		f.Fatal(err)
	}
	var file vectorFile
	if err := json.Unmarshal(data, &file); err != nil {
		f.Fatal(err)
	}
	keys := make([]crypto.PubKey, 0, len(file.Cases))
	for _, c := range file.Cases {
		raw, err := hex.DecodeString(c.VerifierConsensusPub)
		if err != nil {
			f.Fatal(err)
		}
		keys = append(keys, cmted25519.PubKey(raw))
	}
	return file.Cases, keys
}

// checkAcceptedCert re-derives, independently of verifyCertAt, what an accepted
// certificate must satisfy: the endorsement verifies under expected (and under
// no other key or chain ID), the certificate key is the signed TLS key, the
// window is bounded and matches the certificate, and serverAuth is present.
// With checkNow, now must also lie in the window, widened by clock skew.
func checkAcceptedCert(t *testing.T, cert *x509.Certificate, expected crypto.PubKey, others []crypto.PubKey, chainID string, now time.Time, checkNow bool) {
	t.Helper()
	var ext []byte
	for _, e := range cert.Extensions {
		if e.Id.Equal(asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 66463, 1, 1}) {
			ext = e.Value
			break
		}
	}
	var id signedIdentity
	if rest, err := asn1.Unmarshal(ext, &id); err != nil || len(rest) != 0 {
		t.Fatalf("accepted a malformed identity extension: %v", err)
	}
	var bp bindingPayload
	if rest, err := asn1.Unmarshal(id.Payload, &bp); err != nil || len(rest) != 0 {
		t.Fatalf("accepted a malformed binding payload: %v", err)
	}

	envelope, err := core.RawBytesMessageSignBytes(chainID, SignUniqueID, append([]byte(SignPrefix), id.Payload...))
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(ed25519.PublicKey(expected.Bytes()), envelope, id.Signature) {
		t.Fatal("accepted an endorsement that does not verify under the expected key")
	}
	for _, other := range others {
		if !bytes.Equal(other.Bytes(), expected.Bytes()) && verifyCertAt(cert, other, chainID, now) == nil {
			t.Fatal("certificate also accepted under another validator key")
		}
	}
	if verifyCertAt(cert, expected, chainID+"-x", now) == nil {
		t.Fatal("certificate also accepted under another chain ID")
	}

	certKey, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil || !bytes.Equal(certKey, bp.TLSPubKey) {
		t.Fatal("accepted a certificate whose key differs from the signed TLS key")
	}
	if bp.Version != bindingVersion {
		t.Fatalf("accepted binding version %d", bp.Version)
	}
	if bp.NotAfter <= bp.NotBefore || bp.NotAfter-bp.NotBefore > int64(MaxCertValidity/time.Second) {
		t.Fatalf("accepted window [%d, %d]", bp.NotBefore, bp.NotAfter)
	}
	skew := int64(clockSkew / time.Second)
	if checkNow && (now.Unix() < bp.NotBefore-skew || now.Unix() > bp.NotAfter+skew) {
		t.Fatalf("accepted at %d outside window [%d, %d]", now.Unix(), bp.NotBefore, bp.NotAfter)
	}
	if cert.NotBefore.Unix() != bp.NotBefore || cert.NotAfter.Unix() != bp.NotAfter {
		t.Fatal("accepted a certificate validity that differs from the signed window")
	}
	if !slices.Contains(cert.ExtKeyUsage, x509.ExtKeyUsageServerAuth) {
		t.Fatal("accepted a certificate without serverAuth")
	}
}

// FuzzVerifyPeerCert feeds arbitrary and mutated peer certificate DER, seeded
// with the golden vectors, to VerifyPeer and verifyCertAt. Fibre clients run
// this on every server they dial. Neither may panic, and any acceptance must
// satisfy checkAcceptedCert.
func FuzzVerifyPeerCert(f *testing.F) {
	cases, keys := loadFuzzVectors(f)
	for i, c := range cases {
		der, err := hex.DecodeString(c.CertDER)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(der, byte(i), c.VerifierChainID != vectorChainID, c.VerifyAt-vectorT0)
	}
	f.Add([]byte{}, byte(0), false, int64(0))
	f.Add([]byte{0x30, 0x03, 0x02, 0x01, 0x01}, byte(0), false, int64(0))

	f.Fuzz(func(t *testing.T, der []byte, keySel byte, otherChain bool, nowOffset int64) {
		expected := keys[int(keySel)%len(keys)]
		chainID := vectorChainID
		if otherChain {
			chainID = fuzzOtherChainID
		}

		// VerifyPeer checks against the wall clock.
		if VerifyPeer(expected, chainID)([][]byte{der}, nil) == nil {
			cert, err := x509.ParseCertificate(der)
			if err != nil {
				t.Fatalf("VerifyPeer accepted unparsable DER: %v", err)
			}
			checkAcceptedCert(t, cert, expected, keys, chainID, time.Now(), false)
		}

		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return
		}
		now := time.Unix(vectorT0, 0).Add(time.Duration(nowOffset%(1<<40)) * time.Second)
		if verifyCertAt(cert, expected, chainID, now) == nil {
			checkAcceptedCert(t, cert, expected, keys, chainID, now, true)
		}
	})
}

// Flag bits for FuzzVerifyIdentityExtension.
const (
	fuzzSign        = 1 << iota // endorse the payload with the consensus key
	fuzzRawPayload              // use the raw bytes as the binding payload
	fuzzRawExt                  // use the raw bytes as the whole extension
	fuzzOtherTLSKey             // sign a different TLS key than the cert's
	fuzzOtherSigner             // verify against a different validator key
	fuzzOtherChain              // verify under a different chain ID
	fuzzNoServerEKU             // omit serverAuth from the certificate
	fuzzCertWindow              // shift the certificate window off the signed one
)

// FuzzVerifyIdentityExtension builds a well-formed certificate around a
// fuzzer-chosen identity endorsement, so verifyCertAt's checks run past
// certificate parsing on every input. With a structured payload the expected
// verdict is computed independently and must match exactly; with raw bytes
// any acceptance must satisfy checkAcceptedCert.
func FuzzVerifyIdentityExtension(f *testing.F) {
	day := int64(24 * 3600)
	validity := int64(CertValidity / time.Second)
	f.Add(byte(fuzzSign), int64(1), int64(-300), validity, int64(3600), []byte{}, []byte{}, int8(0))
	f.Add(byte(fuzzSign), int64(1), int64(0), int64(MaxCertValidity/time.Second), int64(-300), []byte{}, []byte{}, int8(0))
	f.Add(byte(fuzzSign), int64(1), int64(0), day, day+300, []byte{}, []byte{}, int8(0))
	f.Add(byte(fuzzSign|fuzzCertWindow), int64(1), int64(0), day, int64(10), []byte{}, []byte{}, int8(1))
	f.Add(byte(fuzzSign|fuzzOtherTLSKey|fuzzNoServerEKU), int64(2), int64(0), day, int64(10), []byte{}, []byte{}, int8(0))
	f.Add(byte(0), int64(1), int64(0), day, int64(10), []byte{}, bytes.Repeat([]byte{7}, 64), int8(0))
	f.Add(byte(fuzzSign|fuzzRawPayload), int64(1), int64(0), day, int64(10), []byte{0x30, 0x03, 0x02, 0x01, 0x01}, []byte{}, int8(0))
	f.Add(byte(fuzzRawExt), int64(1), int64(0), day, int64(10), []byte{0x30, 0x04, 0x04, 0x00, 0x04, 0x00}, []byte{}, int8(0))

	consPriv := cmted25519.PrivKey(ed25519.NewKeyFromSeed(vectorSeed(0x10)))
	signer := core.NewMockPVWithParams(consPriv, false, false)
	otherSigner := cmted25519.PrivKey(ed25519.NewKeyFromSeed(vectorSeed(0x11))).PubKey()
	tlsPriv := ed25519.NewKeyFromSeed(vectorSeed(0x50))
	otherTLSPub := ed25519.NewKeyFromSeed(vectorSeed(0x51)).Public()
	tlsDER, err := x509.MarshalPKIXPublicKey(tlsPriv.Public())
	if err != nil {
		f.Fatal(err)
	}
	otherTLSDER, err := x509.MarshalPKIXPublicKey(otherTLSPub)
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, flags byte, version, notBefore, notAfter, nowOffset int64, raw, sig []byte, certShift int8) {
		// Offsets from vectorT0 stay within a few centuries so certificate
		// times remain encodable.
		var err error
		const span = int64(1) << 32
		nb := vectorT0 + notBefore%span
		na := vectorT0 + notAfter%span
		now := vectorT0 + nowOffset%span

		payload := raw
		if flags&fuzzRawPayload == 0 {
			signedTLS := tlsDER
			if flags&fuzzOtherTLSKey != 0 {
				signedTLS = otherTLSDER
			}
			payload, err = asn1.Marshal(bindingPayload{Version: int(int8(version)), NotBefore: nb, NotAfter: na, TLSPubKey: signedTLS})
			if err != nil {
				t.Fatal(err)
			}
		}
		if flags&fuzzSign != 0 {
			if sig, err = signer.SignRawBytes(vectorChainID, SignUniqueID, signedBytes(payload)); err != nil {
				t.Fatal(err)
			}
		}
		ext := raw
		if flags&fuzzRawExt == 0 {
			if ext, err = asn1.Marshal(signedIdentity{Payload: payload, Signature: sig}); err != nil {
				return // asn1 cannot encode empty fields only in edge cases
			}
		}

		certNB, certNA := nb, na
		if flags&fuzzCertWindow != 0 {
			certNB += int64(certShift)
			certNA -= int64(certShift) / 2
		}
		eku := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
		if flags&fuzzNoServerEKU != 0 {
			eku = eku[1:]
		}
		tmpl := &x509.Certificate{
			SerialNumber:    big.NewInt(1),
			Subject:         pkix.Name{CommonName: vectorCertName},
			NotBefore:       time.Unix(certNB, 0),
			NotAfter:        time.Unix(certNA, 0),
			KeyUsage:        x509.KeyUsageDigitalSignature,
			ExtKeyUsage:     eku,
			ExtraExtensions: []pkix.Extension{{Id: signedIDExtensionOID, Value: ext}},
		}
		der, err := x509.CreateCertificate(nil, tmpl, tmpl, tlsPriv.Public(), tlsPriv)
		if err != nil {
			return
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return
		}

		expected, chainID := signer.PrivKey.PubKey(), vectorChainID
		if flags&fuzzOtherSigner != 0 {
			expected = otherSigner
		}
		if flags&fuzzOtherChain != 0 {
			chainID = fuzzOtherChainID
		}
		at := time.Unix(now, 0)
		verifyErr := verifyCertAt(cert, expected, chainID, at)
		if verifyErr == nil {
			checkAcceptedCert(t, cert, expected, []crypto.PubKey{signer.PrivKey.PubKey(), otherSigner}, chainID, at, true)
		}
		if flags&(fuzzRawPayload|fuzzRawExt) != 0 {
			return
		}

		skew := int64(clockSkew / time.Second)
		want := flags&(fuzzOtherTLSKey|fuzzOtherSigner|fuzzOtherChain|fuzzNoServerEKU) == 0 &&
			int8(version) == bindingVersion &&
			na > nb && na-nb <= int64(MaxCertValidity/time.Second) &&
			now >= nb-skew && now <= na+skew &&
			certNB == nb && certNA == na
		// A fuzzed signature is never a valid endorsement.
		want = want && flags&fuzzSign != 0
		if want != (verifyErr == nil) {
			t.Fatalf("verifyCertAt = %v, want accept=%t (flags %08b, window [%d, %d], now %d)", verifyErr, want, flags, nb, na, now)
		}
	})
}
