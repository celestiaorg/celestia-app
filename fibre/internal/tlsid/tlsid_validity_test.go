package tlsid

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"testing"
	"time"

	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These are white-box tests (package tlsid) that exercise the validity-window
// enforcement: they mint certificates with controlled validity via the
// unexported buildServerCert and forgeCert helpers.

func TestVerify_AcceptsWithinWindow(t *testing.T) {
	pv := core.NewMockPV()
	cert, err := buildServerCert(pv, time.Now(), CertValidity)
	require.NoError(t, err)

	pub, err := pv.GetPubKey()
	require.NoError(t, err)
	require.NoError(t, VerifyPeer(pub)(cert.Certificate, nil))
}

func TestVerify_RejectsExpiredCert(t *testing.T) {
	pv := core.NewMockPV()
	// Issued 48h ago with a 24h window: long expired by now.
	cert, err := buildServerCert(pv, time.Now().Add(-48*time.Hour), 24*time.Hour)
	require.NoError(t, err)

	pub, err := pv.GetPubKey()
	require.NoError(t, err)
	err = VerifyPeer(pub)(cert.Certificate, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not valid at")
}

func TestVerify_RejectsNotYetValidCert(t *testing.T) {
	pv := core.NewMockPV()
	// Issued as if 48h in the future: not yet valid.
	cert, err := buildServerCert(pv, time.Now().Add(48*time.Hour), 24*time.Hour)
	require.NoError(t, err)

	pub, err := pv.GetPubKey()
	require.NoError(t, err)
	err = VerifyPeer(pub)(cert.Certificate, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not valid at")
}

func TestVerify_RejectsOverLongCert(t *testing.T) {
	pv := core.NewMockPV()
	// Currently valid, but with a window that exceeds MaxCertValidity.
	cert, err := buildServerCert(pv, time.Now(), MaxCertValidity+time.Hour)
	require.NoError(t, err)

	pub, err := pv.GetPubKey()
	require.NoError(t, err)
	err = VerifyPeer(pub)(cert.Certificate, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestVerify_RejectsCertValidityDifferentFromSigned(t *testing.T) {
	pv := core.NewMockPV()
	now := time.Now().Truncate(time.Second)
	signedNotBefore := now.Add(-clockSkew)
	signedNotAfter := now.Add(CertValidity)

	// Cert claims a NotAfter far beyond the signed window.
	cert := forgeCert(t, pv, forgeOpts{
		signedNotBefore: signedNotBefore,
		signedNotAfter:  signedNotAfter,
		certNotBefore:   signedNotBefore,
		certNotAfter:    now.Add(72 * time.Hour),
		eku:             []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})

	pub, err := pv.GetPubKey()
	require.NoError(t, err)
	err = verifyCert(cert, pub)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "certificate validity does not match signed identity")
}

func TestVerify_RejectsMissingServerAuthEKU(t *testing.T) {
	pv := core.NewMockPV()
	now := time.Now().Truncate(time.Second)
	signedNotBefore := now.Add(-clockSkew)
	signedNotAfter := now.Add(CertValidity)

	cert := forgeCert(t, pv, forgeOpts{
		signedNotBefore: signedNotBefore,
		signedNotAfter:  signedNotAfter,
		certNotBefore:   signedNotBefore,
		certNotAfter:    signedNotAfter,
		eku:             []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, // no serverAuth
	})

	pub, err := pv.GetPubKey()
	require.NoError(t, err)
	err = verifyCert(cert, pub)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "serverAuth")
}

type forgeOpts struct {
	signedNotBefore time.Time
	signedNotAfter  time.Time
	certNotBefore   time.Time
	certNotAfter    time.Time
	eku             []x509.ExtKeyUsage
}

// forgeCert mints a self-signed certificate whose embedded identity is validly
// signed by signer, but whose certificate template fields (validity, EKU) can
// diverge from the signed binding. The cert's TLS key matches the signed
// tlsPubKey so verification reaches the field-consistency checks.
func forgeCert(t *testing.T, signer core.PrivValidator, o forgeOpts) *x509.Certificate {
	t.Helper()

	tlsPub, tlsPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	tlsPubDER, err := x509.MarshalPKIXPublicKey(tlsPub)
	require.NoError(t, err)

	payloadDER, err := asn1.Marshal(bindingPayload{
		Version:   bindingVersion,
		NotBefore: o.signedNotBefore.Unix(),
		NotAfter:  o.signedNotAfter.Unix(),
		TLSPubKey: tlsPubDER,
	})
	require.NoError(t, err)

	sig, err := signer.SignRawBytes(signDomain, SignUniqueID, signedBytes(payloadDER))
	require.NoError(t, err)

	extBytes, err := asn1.Marshal(signedIdentity{Payload: payloadDER, Signature: sig})
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(99),
		Subject:      pkix.Name{CommonName: "forged-fibre"},
		NotBefore:    o.certNotBefore,
		NotAfter:     o.certNotAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  o.eku,
		ExtraExtensions: []pkix.Extension{{
			Id:    signedIDExtensionOID,
			Value: extBytes,
		}},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, tlsPub, tlsPriv)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(certDER)
	require.NoError(t, err)
	return cert
}
