package tlsid

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These are the golden vectors for the wire format specified in
// specs/src/fibre_tls_identity.md. Generation is fully deterministic (fixed
// key seeds, serials, and timestamps; Ed25519 signing is deterministic), so
// the test regenerates every case from vectorSpecs and requires byte-equality
// with the committed testdata/identity_vectors.json — a change to the OID,
// the ASN.1 layout, the sign-bytes envelope, or the verifier semantics fails
// here. Regenerate deliberately with:
//
//	go test ./fibre/internal/tlsid -run TestGoldenVectors -update

var updateVectors = flag.Bool("update", false, "regenerate testdata/identity_vectors.json")

const (
	vectorsPath    = "testdata/identity_vectors.json"
	vectorChainID  = "celestia-vectors"
	vectorT0       = int64(1767225600) // 2026-01-01T00:00:00Z
	vectorCertName = "celestia-fibre"
)

type vectorFile struct {
	Description string          `json:"description"`
	Constants   vectorConstants `json:"constants"`
	Cases       []vectorCase    `json:"cases"`
}

type vectorConstants struct {
	ExtensionOID             string `json:"extension_oid"`
	SignUniqueID             string `json:"sign_unique_id"`
	SignPrefix               string `json:"sign_prefix"`
	EnvelopePrefix           string `json:"envelope_prefix"`
	BindingVersion           int    `json:"binding_version"`
	MaxIdentityExtensionSize int    `json:"max_identity_extension_size"`
	MaxPayloadDERSize        int    `json:"max_payload_der_size"`
	MaxCertValiditySeconds   int64  `json:"max_cert_validity_seconds"`
	ClockSkewSeconds         int64  `json:"clock_skew_seconds"`
}

type vectorBinding struct {
	Version   int   `json:"version"`
	NotBefore int64 `json:"not_before"`
	NotAfter  int64 `json:"not_after"`
}

type vectorExpected struct {
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

type vectorCase struct {
	Name                 string         `json:"name"`
	Description          string         `json:"description"`
	ChainID              string         `json:"chain_id"`
	ConsensusPrivSeed    string         `json:"consensus_priv_seed"`
	ConsensusPub         string         `json:"consensus_pub"`
	TLSPrivSeed          string         `json:"tls_priv_seed"`
	TLSPubRaw            string         `json:"tls_pub_raw"`
	TLSPubSPKIDER        string         `json:"tls_pub_spki_der"`
	Binding              *vectorBinding `json:"binding,omitempty"`
	PayloadDER           string         `json:"payload_der"`
	SignInput            string         `json:"sign_input"`
	SignedBytes          string         `json:"signed_bytes"`
	Signature            string         `json:"signature"`
	ExtensionDER         string         `json:"extension_der"`
	CertSerial           string         `json:"cert_serial"`
	CertDER              string         `json:"cert_der"`
	VerifierChainID      string         `json:"verifier_chain_id"`
	VerifierConsensusPub string         `json:"verifier_consensus_pub"`
	VerifyAt             int64          `json:"verify_at"`
	Expected             vectorExpected `json:"expected"`
}

// vectorErrSubstrings maps the stable error enum published in the vectors to
// the Go error text produced by verifyCertAt. Non-Go implementations match on
// the enum; only this test knows the Go strings.
var vectorErrSubstrings = map[string]string{
	"extension_missing":       "missing the fibre identity extension",
	"extension_too_large":     "identity extension size",
	"extension_malformed":     "unmarshal identity extension",
	"extension_trailing_data": "trailing bytes in identity extension",
	"payload_empty":           "empty identity payload",
	"payload_too_large":       "identity payload size",
	"signature_empty":         "empty identity signature",
	"payload_malformed":       "unmarshal binding payload",
	"binding_trailing_data":   "trailing bytes in binding payload",
	"unsupported_version":     "unsupported fibre identity version",
	"signature_invalid":       "signature is invalid",
	"tls_key_mismatch":        "public key does not match signed identity",
	"window_empty":            "validity window is empty",
	"window_too_long":         "exceeds maximum",
	"outside_validity_window": "not valid at",
	"cert_window_mismatch":    "certificate validity does not match signed identity",
	"eku_missing":             "serverAuth",
}

// vectorSpec holds the generation knobs for one case. Only its outputs land
// in the JSON; knobs that merely shape a forged certificate (certTLSSeed,
// eku, cert window overrides) stay here.
type vectorSpec struct {
	name          string
	description   string
	consensusSeed byte // repeated to a 32-byte Ed25519 seed
	tlsSeed       byte
	serial        int64

	// production path: mint via buildServerCertWithKey(issue, validity).
	productionPath bool
	issue          int64
	validity       time.Duration

	// forge path: explicit binding window and cert-shape overrides.
	notBefore, notAfter         int64 // 0,0 → honest window
	bindingVersion              int   // 0 → bindingVersion constant
	certNotBefore, certNotAfter int64 // 0 → binding values
	certTLSSeed                 byte  // 0 → tlsSeed
	eku                         []x509.ExtKeyUsage
	tamperSig                   bool
	emptySig                    bool
	rawPayload                  []byte // non-nil → replaces the BindingPayload DER
	payloadTrailingByte         bool   // trailing byte inside the signed payload
	rawExtension                []byte // non-nil → replaces the SignedIdentity DER
	extensionTrailingByte       bool   // trailing byte after the SignedIdentity DER
	omitExtension               bool   // certificate carries no identity extension

	// verifier inputs.
	verifierChainID string // "" → producer chain ID
	verifierPubSeed byte   // 0 → consensusSeed
	verifyAt        int64
	expected        vectorExpected
}

func vectorSpecs() []vectorSpec {
	certValiditySecs := int64(CertValidity / time.Second)

	// Cases are ordered by the spec's verification-check numbering; every
	// MUST-reject rule in specs/src/fibre_tls_identity.md has at least one
	// case, so deleting a check from verifyCertAt fails this test.
	specs := []vectorSpec{
		{
			name:           "valid",
			description:    "production-shaped certificate; verification succeeds",
			productionPath: true,
			issue:          vectorT0,
			validity:       CertValidity,
			verifyAt:       vectorT0 + 3600,
			expected:       vectorExpected{Valid: true},
		},
		{
			name:          "extension_missing",
			description:   "certificate carries no fibre identity extension",
			omitExtension: true,
			verifyAt:      vectorT0 + 3600,
			expected:      vectorExpected{Error: "extension_missing"},
		},
		{
			name:        "extension_too_large",
			description: "identity extension exceeds MaxIdentityExtensionSize",
			rawPayload:  bytes.Repeat([]byte{0xaa}, MaxIdentityExtensionSize+128),
			verifyAt:    vectorT0 + 3600,
			expected:    vectorExpected{Error: "extension_too_large"},
		},
		{
			name:         "extension_malformed",
			description:  "extension value is not a SignedIdentity DER",
			rawExtension: []byte{0xde, 0xad, 0xbe, 0xef},
			verifyAt:     vectorT0 + 3600,
			expected:     vectorExpected{Error: "extension_malformed"},
		},
		{
			name:                  "extension_trailing_data",
			description:           "trailing byte after the SignedIdentity DER in the extension",
			extensionTrailingByte: true,
			verifyAt:              vectorT0 + 3600,
			expected:              vectorExpected{Error: "extension_trailing_data"},
		},
		{
			name:        "payload_empty",
			description: "SignedIdentity payload is an empty OCTET STRING",
			rawPayload:  []byte{},
			verifyAt:    vectorT0 + 3600,
			expected:    vectorExpected{Error: "payload_empty"},
		},
		{
			name:        "payload_too_large",
			description: "SignedIdentity payload exceeds MaxPayloadDERSize",
			rawPayload:  bytes.Repeat([]byte{0xbb}, MaxPayloadDERSize+128),
			verifyAt:    vectorT0 + 3600,
			expected:    vectorExpected{Error: "payload_too_large"},
		},
		{
			name:        "signature_empty",
			description: "SignedIdentity signature is an empty OCTET STRING",
			emptySig:    true,
			verifyAt:    vectorT0 + 3600,
			expected:    vectorExpected{Error: "signature_empty"},
		},
		{
			name:        "payload_malformed",
			description: "SignedIdentity payload is not a BindingPayload DER",
			rawPayload:  []byte{0xca, 0xfe, 0xf0, 0x0d},
			verifyAt:    vectorT0 + 3600,
			expected:    vectorExpected{Error: "payload_malformed"},
		},
		{
			name:                "binding_trailing_data",
			description:         "trailing byte after the BindingPayload DER inside the signed payload",
			payloadTrailingByte: true,
			verifyAt:            vectorT0 + 3600,
			expected:            vectorExpected{Error: "binding_trailing_data"},
		},
		{
			name:           "unknown_version",
			description:    "binding payload carries version 2 (validly signed)",
			bindingVersion: 2,
			verifyAt:       vectorT0 + 3600,
			expected:       vectorExpected{Error: "unsupported_version"},
		},
		{
			name:        "signature_tampered",
			description: "one bit flipped in the endorsement signature",
			tamperSig:   true,
			verifyAt:    vectorT0 + 3600,
			expected:    vectorExpected{Error: "signature_invalid"},
		},
		{
			name:            "wrong_validator",
			description:     "verifier expects a different validator consensus key",
			productionPath:  true,
			issue:           vectorT0,
			validity:        CertValidity,
			verifierPubSeed: 0x99,
			verifyAt:        vectorT0 + 3600,
			expected:        vectorExpected{Error: "signature_invalid"},
		},
		{
			name:            "wrong_chain_id",
			description:     "verifier uses a different chain ID in the signing envelope",
			productionPath:  true,
			issue:           vectorT0,
			validity:        CertValidity,
			verifierChainID: vectorChainID + "-other",
			verifyAt:        vectorT0 + 3600,
			expected:        vectorExpected{Error: "signature_invalid"},
		},
		{
			name:        "tls_key_mismatch",
			description: "certificate presents a different TLS key than the signed tlsPubKey",
			certTLSSeed: 0xa5,
			verifyAt:    vectorT0 + 3600,
			expected:    vectorExpected{Error: "tls_key_mismatch"},
		},
		{
			name:        "window_empty",
			description: "signed notAfter equals notBefore",
			notBefore:   vectorT0,
			notAfter:    vectorT0,
			verifyAt:    vectorT0,
			expected:    vectorExpected{Error: "window_empty"},
		},
		{
			name:           "window_too_long",
			description:    "signed validity window exceeds MaxCertValidity",
			productionPath: true,
			issue:          vectorT0,
			validity:       MaxCertValidity + time.Hour,
			verifyAt:       vectorT0 + 3600,
			expected:       vectorExpected{Error: "window_too_long"},
		},
		{
			name:           "expired",
			description:    "verification time is after notAfter plus clock skew",
			productionPath: true,
			issue:          vectorT0 - 172800,
			validity:       24 * time.Hour,
			verifyAt:       vectorT0,
			expected:       vectorExpected{Error: "outside_validity_window"},
		},
		{
			name:           "not_yet_valid",
			description:    "verification time is before notBefore minus clock skew",
			productionPath: true,
			issue:          vectorT0 + 172800,
			validity:       24 * time.Hour,
			verifyAt:       vectorT0,
			expected:       vectorExpected{Error: "outside_validity_window"},
		},
		{
			name:         "cert_window_mismatch",
			description:  "certificate NotAfter differs from the signed notAfter",
			certNotAfter: vectorT0 + certValiditySecs + 259200,
			verifyAt:     vectorT0 + 3600,
			expected:     vectorExpected{Error: "cert_window_mismatch"},
		},
		{
			name:        "eku_missing",
			description: "certificate lacks the serverAuth extended key usage",
			eku:         []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			verifyAt:    vectorT0 + 3600,
			expected:    vectorExpected{Error: "eku_missing"},
		},
	}

	for i := range specs {
		s := &specs[i]
		s.consensusSeed = byte(0x10 + i)
		s.tlsSeed = byte(0x50 + i)
		s.serial = int64(1001 + i)
		if !s.productionPath && s.notBefore == 0 && s.notAfter == 0 {
			s.notBefore = vectorT0 - int64(clockSkew/time.Second)
			s.notAfter = vectorT0 + certValiditySecs
		}
	}
	return specs
}

func vectorSeed(b byte) []byte {
	return bytes.Repeat([]byte{b}, ed25519.SeedSize)
}

func buildVectorCase(t *testing.T, s vectorSpec) vectorCase {
	t.Helper()

	consSeed := vectorSeed(s.consensusSeed)
	consPriv := ed25519.NewKeyFromSeed(consSeed)
	consPub := consPriv.Public().(ed25519.PublicKey)

	tlsSeed := vectorSeed(s.tlsSeed)
	tlsPriv := ed25519.NewKeyFromSeed(tlsSeed)
	tlsPub := tlsPriv.Public().(ed25519.PublicKey)
	tlsPubDER, err := x509.MarshalPKIXPublicKey(tlsPub)
	require.NoError(t, err)

	notBefore, notAfter := s.notBefore, s.notAfter
	if s.productionPath {
		notBefore = s.issue - int64(clockSkew/time.Second)
		notAfter = s.issue + int64(s.validity/time.Second)
	}
	version := s.bindingVersion
	if version == 0 {
		version = bindingVersion
	}

	payloadDER := s.rawPayload
	if payloadDER == nil {
		payloadDER, err = asn1.Marshal(bindingPayload{
			Version:   version,
			NotBefore: notBefore,
			NotAfter:  notAfter,
			TLSPubKey: tlsPubDER,
		})
		require.NoError(t, err)
		if s.payloadTrailingByte {
			payloadDER = append(payloadDER, 0x00)
		}
	}

	signInput := signedBytes(payloadDER)
	envelope, err := core.RawBytesMessageSignBytes(vectorChainID, SignUniqueID, signInput)
	require.NoError(t, err)
	sig := ed25519.Sign(consPriv, envelope)

	embedSig := sig
	switch {
	case s.tamperSig:
		embedSig = bytes.Clone(sig)
		embedSig[0] ^= 0x01
	case s.emptySig:
		embedSig = []byte{}
	}

	extDER := s.rawExtension
	if extDER == nil {
		extDER, err = asn1.Marshal(signedIdentity{Payload: payloadDER, Signature: embedSig})
		require.NoError(t, err)
		if s.extensionTrailingByte {
			extDER = append(extDER, 0x00)
		}
	}

	serial := big.NewInt(s.serial)
	var certDER []byte
	if s.productionPath {
		pv := core.NewMockPVWithParams(cmted25519.PrivKey(consPriv), false, false)
		tlsCert, err := buildServerCertWithKey(pv, vectorChainID,
			time.Unix(s.issue, 0).UTC(), s.validity, tlsPub, tlsPriv, serial)
		require.NoError(t, err)
		certDER = tlsCert.Certificate[0]

		// The production path must emit exactly the extension computed above;
		// this equality is what pins the producer encoding to the vectors.
		parsed, err := x509.ParseCertificate(certDER)
		require.NoError(t, err)
		var embedded []byte
		for _, ext := range parsed.Extensions {
			if ext.Id.Equal(signedIDExtensionOID) {
				embedded = ext.Value
			}
		}
		require.Equal(t, extDER, embedded, "case %s: buildServerCertWithKey extension drifted", s.name)
	} else {
		certNotBefore, certNotAfter := notBefore, notAfter
		if s.certNotBefore != 0 {
			certNotBefore = s.certNotBefore
		}
		if s.certNotAfter != 0 {
			certNotAfter = s.certNotAfter
		}
		certPriv, certPub := tlsPriv, tlsPub
		if s.certTLSSeed != 0 {
			certPriv = ed25519.NewKeyFromSeed(vectorSeed(s.certTLSSeed))
			certPub = certPriv.Public().(ed25519.PublicKey)
		}
		eku := s.eku
		if eku == nil {
			eku = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
		}
		tmpl := &x509.Certificate{
			SerialNumber: serial,
			Subject:      pkix.Name{CommonName: vectorCertName},
			NotBefore:    time.Unix(certNotBefore, 0).UTC(),
			NotAfter:     time.Unix(certNotAfter, 0).UTC(),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  eku,
		}
		if !s.omitExtension {
			tmpl.ExtraExtensions = []pkix.Extension{{
				Id:    signedIDExtensionOID,
				Value: extDER,
			}}
		}
		certDER, err = x509.CreateCertificate(rand.Reader, tmpl, tmpl, certPub, certPriv)
		require.NoError(t, err)
	}

	verifierChainID := s.verifierChainID
	if verifierChainID == "" {
		verifierChainID = vectorChainID
	}
	verifierPub := consPub
	if s.verifierPubSeed != 0 {
		verifierPub = ed25519.NewKeyFromSeed(vectorSeed(s.verifierPubSeed)).Public().(ed25519.PublicKey)
	}

	c := vectorCase{
		Name:              s.name,
		Description:       s.description,
		ChainID:           vectorChainID,
		ConsensusPrivSeed: hex.EncodeToString(consSeed),
		ConsensusPub:      hex.EncodeToString(consPub),
		TLSPrivSeed:       hex.EncodeToString(tlsSeed),
		TLSPubRaw:         hex.EncodeToString(tlsPub),
		TLSPubSPKIDER:     hex.EncodeToString(tlsPubDER),
		Binding: &vectorBinding{
			Version:   version,
			NotBefore: notBefore,
			NotAfter:  notAfter,
		},
		PayloadDER:           hex.EncodeToString(payloadDER),
		SignInput:            hex.EncodeToString(signInput),
		SignedBytes:          hex.EncodeToString(envelope),
		Signature:            hex.EncodeToString(embedSig),
		ExtensionDER:         hex.EncodeToString(extDER),
		CertSerial:           serial.String(),
		CertDER:              hex.EncodeToString(certDER),
		VerifierChainID:      verifierChainID,
		VerifierConsensusPub: hex.EncodeToString(verifierPub),
		VerifyAt:             s.verifyAt,
		Expected:             s.expected,
	}

	// Blank the layers a case corrupts or omits: fields describe what is
	// actually in the certificate, so a payload that is not a BindingPayload
	// has no binding, and a garbage or missing extension has no payload
	// fields. Mirrors the schema notes in the spec.
	if s.rawPayload != nil || s.rawExtension != nil || s.omitExtension {
		c.Binding = nil
	}
	if s.rawExtension != nil || s.omitExtension {
		c.PayloadDER, c.SignInput, c.SignedBytes, c.Signature = "", "", "", ""
	}
	if s.omitExtension {
		c.ExtensionDER = ""
	}
	return c
}

func buildVectorFile(t *testing.T) vectorFile {
	t.Helper()
	file := vectorFile{
		Description: "Golden vectors for the Fibre validator-endorsed TLS identity (celestia-fibre-tls-v1). Normative spec: specs/src/fibre_tls_identity.md.",
		Constants: vectorConstants{
			ExtensionOID:             signedIDExtensionOID.String(),
			SignUniqueID:             SignUniqueID,
			SignPrefix:               SignPrefix,
			EnvelopePrefix:           core.RawBytesSignBytesPrefix,
			BindingVersion:           bindingVersion,
			MaxIdentityExtensionSize: MaxIdentityExtensionSize,
			MaxPayloadDERSize:        MaxPayloadDERSize,
			MaxCertValiditySeconds:   int64(MaxCertValidity / time.Second),
			ClockSkewSeconds:         int64(clockSkew / time.Second),
		},
	}
	for _, spec := range vectorSpecs() {
		file.Cases = append(file.Cases, buildVectorCase(t, spec))
	}
	return file
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

func TestGoldenVectors(t *testing.T) {
	built := buildVectorFile(t)

	if *updateVectors {
		data, err := json.MarshalIndent(built, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Dir(vectorsPath), 0o755))
		require.NoError(t, os.WriteFile(vectorsPath, append(data, '\n'), 0o644))
		t.Logf("regenerated %s", vectorsPath)
	}

	data, err := os.ReadFile(vectorsPath)
	require.NoError(t, err, "missing %s; generate it with -update", vectorsPath)
	var committed vectorFile
	require.NoError(t, json.Unmarshal(data, &committed))

	// Producer direction: regenerating from fixed inputs must reproduce the
	// committed file byte-for-byte. Catches drift in the OID, ASN.1 layout,
	// sign-bytes envelope, certificate template, and the pinned constants.
	require.Equal(t, built.Constants, committed.Constants, "protocol constants drifted")
	require.Equal(t, len(built.Cases), len(committed.Cases), "vector case set changed")
	for i := range built.Cases {
		require.Equal(t, committed.Cases[i], built.Cases[i],
			"case %q drifted from committed vectors", committed.Cases[i].Name)
	}

	// Verifier direction: the committed certificates must produce the expected
	// verdicts at the committed verification times. Catches semantics drift.
	for _, c := range committed.Cases {
		t.Run(c.Name, func(t *testing.T) {
			cert, err := x509.ParseCertificate(mustHex(t, c.CertDER))
			require.NoError(t, err)
			expectedPub := cmted25519.PubKey(mustHex(t, c.VerifierConsensusPub))

			verifyErr := verifyCertAt(cert, expectedPub, c.VerifierChainID, time.Unix(c.VerifyAt, 0))
			if c.Expected.Valid {
				require.NoError(t, verifyErr)
				return
			}
			require.Error(t, verifyErr)
			substr, ok := vectorErrSubstrings[c.Expected.Error]
			require.True(t, ok, "unknown expected error enum %q", c.Expected.Error)
			assert.Contains(t, verifyErr.Error(), substr)
		})
	}
}
