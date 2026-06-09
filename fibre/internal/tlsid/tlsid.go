// Package tlsid produces and verifies fibre gRPC TLS certificates that bind
// the ephemeral TLS keypair to a validator's consensus identity.
//
// A fibre server generates a fresh Ed25519 keypair on every start. The
// self-signed certificate it presents carries a custom, non-critical X.509
// extension with the ASN.1 DER layout:
//
//	SignedIdentity ::= SEQUENCE {
//	    payload   OCTET STRING,  -- DER of BindingPayload (the exact signed bytes)
//	    signature OCTET STRING   -- consensus-key signature over the payload
//	}
//
//	BindingPayload ::= SEQUENCE {
//	    version    INTEGER,      -- schema version
//	    notBefore  INTEGER,      -- unix seconds; equals the cert NotBefore
//	    notAfter   INTEGER,      -- unix seconds; equals the cert NotAfter
//	    tlsPubKey  OCTET STRING  -- DER SubjectPublicKeyInfo of the TLS key
//	}
//
// The signature is produced by [core.PrivValidator.SignRawBytes] over
// [SignPrefix] concatenated with the DER-encoded BindingPayload, using
// [SignUniqueID] for domain separation. The verifier checks the embedded payload
// bytes directly (it does not re-encode), then enforces that:
//
//   - the endorsement signature verifies under the expected validator's
//     consensus pubkey (taken from the validator set). A signature that
//     verifies under that key *is* the proof the validator authorized this TLS
//     key, so the consensus pubkey is not embedded in the certificate;
//   - the presented TLS public key equals the signed tlsPubKey (TLS 1.3
//     CertificateVerify proves possession of it, closing the binding);
//   - now is within the signed validity window, the window does not exceed
//     [MaxCertValidity], and the certificate's own NotBefore/NotAfter equal the
//     signed window;
//   - the certificate carries the serverAuth extended key usage.
//
// The BindingPayload deliberately does NOT include the chain ID. The TLS layer
// only proves "this peer is validator V"; chain and data semantics are enforced
// by the chain-bound, consensus-key-signed application messages (payment
// promises, validator acknowledgements) and by on-chain data-availability
// commitments. The outer SignRawBytes envelope still uses the runtime chain ID
// so the endorsement is compatible with chain-ID-enforcing remote signers.
//
// This is a Celestia-specific identity scheme; it is NOT libp2p-TLS compatible
// (different OID, signing prefix, sign-byte wrapper, and pubkey encoding), so a
// stock libp2p verifier will not accept these certificates. It is intentionally
// host-agnostic: nothing about the network location (IP, DNS name, or SNI) is
// bound, so a validator may be addressed by either an IP literal or a DNS name.
// Identity is the validator consensus key; the network location is only a
// routing hint resolved from the on-chain host registry.
package tlsid

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/cometbft/cometbft/crypto"
	core "github.com/cometbft/cometbft/types"
)

// SignUniqueID is the privval domain-separation tag used when binding the TLS
// keypair to a validator's consensus identity. Bumping this string is a
// protocol-level break.
const SignUniqueID = "celestia-fibre-tls-v1"

// SignPrefix is mixed into the signed payload alongside SignUniqueID so the
// signature can never collide with another payload signed by the same key.
const SignPrefix = "celestia-fibre-tls:"

// bindingVersion is the schema version of the signed BindingPayload. Bumping it
// is a protocol-level break.
const bindingVersion = 1

// MaxPayloadDERSize bounds the signed BindingPayload DER accepted from peer
// certificates. Correctly generated payloads are small; this cap keeps malformed
// certs from forcing unbounded parser/allocation work.
const MaxPayloadDERSize = 4096

// MaxIdentityExtensionSize bounds the custom certificate extension DER.
const MaxIdentityExtensionSize = 8192

// CertValidity is how long generated certificates remain valid. The cert is
// re-minted on every server start (the TLS keypair is ephemeral and lives only
// in process memory), and there is deliberately no in-process refresh: a
// Celestia validator's consensus key cannot rotate (the SDK exposes no
// consensus-key rotation), so the endorsed identity never changes while the
// server runs. The window is therefore set well beyond any realistic uptime so
// a long-running server never presents an expired cert.
const CertValidity = 365 * 24 * time.Hour

// clockSkew backdates NotBefore and is tolerated on both edges of the validity
// window so modest clock differences between peers do not break handshakes.
const clockSkew = 5 * time.Minute

// MaxCertValidity is the largest validity window a client will accept. It is a
// sanity ceiling just above [CertValidity]: a correctly signed endorsement is
// still rejected if it claims a window longer than we would ever issue, so a
// one-time signer misuse cannot mint an arbitrarily long-lived cert.
const MaxCertValidity = CertValidity + 2*clockSkew

// signedIDExtensionOID identifies the custom certificate extension carrying the
// fibre identity endorsement.
//
// TODO(PROTOCO-1808): this is the placeholder IANA PEN 32473, which RFC 5612
// reserves for documentation/example use (so it is unassigned to any real
// organization, unlike the previous 56843 = KASSEX s.r.o.). Replace the
// enterprise number with the Celestia-allocated PEN once registered
// (https://linear.app/celestia/issue/PROTOCO-1808). Bumping this OID is a
// protocol-level break.
var signedIDExtensionOID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 32473, 1, 1}

// signedIdentity is the ASN.1 payload of the custom extension.
type signedIdentity struct {
	Payload   []byte
	Signature []byte
}

// bindingPayload is the structured, signed binding between the ephemeral TLS
// key and its validity window. It is signed by the validator consensus key; the
// signer's identity is established by verifying against the expected validator
// pubkey, so the consensus pubkey itself is not part of the payload.
type bindingPayload struct {
	Version   int
	NotBefore int64
	NotAfter  int64
	TLSPubKey []byte
}

// BuildServerCert generates an ephemeral Ed25519 TLS keypair and a self-signed
// certificate that binds it to the consensus identity exposed by signer.
// The signer is invoked once via SignRawBytes using chainID in the CometBFT
// signing envelope.
func BuildServerCert(signer core.PrivValidator, chainID string) (tls.Certificate, error) {
	return buildServerCert(signer, chainID, time.Now(), CertValidity)
}

func buildServerCert(signer core.PrivValidator, chainID string, now time.Time, validity time.Duration) (tls.Certificate, error) {
	if signer == nil {
		return tls.Certificate{}, errors.New("signer must not be nil")
	}
	if chainID == "" {
		return tls.Certificate{}, errors.New("chain ID must not be empty")
	}

	tlsPub, tlsPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate TLS keypair: %w", err)
	}

	tlsPubDER, err := x509.MarshalPKIXPublicKey(tlsPub)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal TLS pubkey: %w", err)
	}

	// Truncate to second precision so the cert's encoded NotBefore/NotAfter
	// match the signed unix-second values exactly.
	notBefore := now.Add(-clockSkew).Truncate(time.Second)
	notAfter := now.Add(validity).Truncate(time.Second)

	payloadDER, err := asn1.Marshal(bindingPayload{
		Version:   bindingVersion,
		NotBefore: notBefore.Unix(),
		NotAfter:  notAfter.Unix(),
		TLSPubKey: tlsPubDER,
	})
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal binding payload: %w", err)
	}

	sig, err := signer.SignRawBytes(chainID, SignUniqueID, signedBytes(payloadDER))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("sign TLS binding: %w", err)
	}

	extBytes, err := asn1.Marshal(signedIdentity{Payload: payloadDER, Signature: sig})
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal identity extension: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate cert serial: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "celestia-fibre"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		ExtraExtensions: []pkix.Extension{{
			Id:    signedIDExtensionOID,
			Value: extBytes,
		}},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, tlsPub, tlsPriv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create cert: %w", err)
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  tlsPriv,
	}, nil
}

// VerifyPeer returns a [tls.Config.VerifyPeerCertificate] callback that accepts
// a peer iff its leaf certificate carries a validly-signed identity extension
// for expected. Prefer [VerifyConnection], which also runs on resumed TLS 1.3
// sessions. Use only with tls.Config.InsecureSkipVerify=true; the custom
// verifier replaces CA/hostname validation with validator consensus-key binding.
// chainID must match the chain ID used when the server endorsed the certificate.
func VerifyPeer(expected crypto.PubKey, chainID string) func([][]byte, [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return errors.New("peer presented no certificate")
		}
		cert, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("parse peer cert: %w", err)
		}
		return verifyCert(cert, expected, chainID)
	}
}

// VerifyConnection returns a [tls.Config.VerifyConnection] callback equivalent
// to [VerifyPeer]. Unlike VerifyPeerCertificate it also runs for resumed TLS
// 1.3 sessions, so identity is re-checked on every connection.
// chainID must match the chain ID used when the server endorsed the certificate.
func VerifyConnection(expected crypto.PubKey, chainID string) func(tls.ConnectionState) error {
	return func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return errors.New("peer presented no certificate")
		}
		return verifyCert(state.PeerCertificates[0], expected, chainID)
	}
}

func verifyCert(cert *x509.Certificate, expected crypto.PubKey, chainID string) error {
	if expected == nil {
		return errors.New("no expected validator pubkey")
	}
	if chainID == "" {
		return errors.New("chain ID must not be empty")
	}

	var extBytes []byte
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(signedIDExtensionOID) {
			extBytes = ext.Value
			break
		}
	}
	if extBytes == nil {
		return errors.New("peer cert is missing the fibre identity extension")
	}
	if len(extBytes) > MaxIdentityExtensionSize {
		return fmt.Errorf("identity extension size %d exceeds maximum %d", len(extBytes), MaxIdentityExtensionSize)
	}

	var id signedIdentity
	rest, err := asn1.Unmarshal(extBytes, &id)
	if err != nil {
		return fmt.Errorf("unmarshal identity extension: %w", err)
	}
	if len(rest) != 0 {
		return errors.New("trailing bytes in identity extension")
	}
	if len(id.Payload) == 0 {
		return errors.New("empty identity payload")
	}
	if len(id.Payload) > MaxPayloadDERSize {
		return fmt.Errorf("identity payload size %d exceeds maximum %d", len(id.Payload), MaxPayloadDERSize)
	}
	if len(id.Signature) == 0 {
		return errors.New("empty identity signature")
	}

	var bp bindingPayload
	rest, err = asn1.Unmarshal(id.Payload, &bp)
	if err != nil {
		return fmt.Errorf("unmarshal binding payload: %w", err)
	}
	if len(rest) != 0 {
		return errors.New("trailing bytes in binding payload")
	}
	if bp.Version != bindingVersion {
		return fmt.Errorf("unsupported fibre identity version %d", bp.Version)
	}

	// Verify the endorsement signature over the exact embedded payload bytes
	// using the expected validator's consensus pubkey (from the validator set).
	// A signature that verifies under `expected` is the proof the validator
	// authorized this TLS key, so no pubkey needs to be embedded in the cert.
	signed, err := core.RawBytesMessageSignBytes(chainID, SignUniqueID, signedBytes(id.Payload))
	if err != nil {
		return fmt.Errorf("compute signed bytes: %w", err)
	}
	if !expected.VerifySignature(signed, id.Signature) {
		return errors.New("peer cert signature is invalid")
	}

	// Bind the presented TLS key to the signed key; TLS 1.3 proves possession.
	tlsPubDER, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return fmt.Errorf("marshal peer TLS pubkey: %w", err)
	}
	if !bytes.Equal(tlsPubDER, bp.TLSPubKey) {
		return errors.New("peer cert public key does not match signed identity")
	}

	// Enforce the signed validity window and an upper bound on its length.
	notBefore := time.Unix(bp.NotBefore, 0)
	notAfter := time.Unix(bp.NotAfter, 0)
	if !notAfter.After(notBefore) {
		return errors.New("fibre identity validity window is empty")
	}
	if notAfter.Sub(notBefore) > MaxCertValidity {
		return fmt.Errorf("fibre identity validity window %s exceeds maximum %s",
			notAfter.Sub(notBefore), MaxCertValidity)
	}
	now := time.Now()
	if now.Before(notBefore.Add(-clockSkew)) || now.After(notAfter.Add(clockSkew)) {
		return fmt.Errorf("peer fibre identity not valid at %s", now.UTC().Format(time.RFC3339))
	}

	// Bind the certificate's own validity to the signed window so a reissued
	// cert cannot widen it without invalidating the signature.
	if cert.NotBefore.Unix() != bp.NotBefore || cert.NotAfter.Unix() != bp.NotAfter {
		return errors.New("certificate validity does not match signed identity")
	}

	// Require the serverAuth EKU that BuildServerCert sets.
	if !slices.Contains(cert.ExtKeyUsage, x509.ExtKeyUsageServerAuth) {
		return errors.New("peer cert missing serverAuth extended key usage")
	}

	return nil
}

func signedBytes(payloadDER []byte) []byte {
	out := make([]byte, 0, len(SignPrefix))
	out = append(out, SignPrefix...)
	out = append(out, payloadDER...)
	return out
}
