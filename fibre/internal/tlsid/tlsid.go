// Package tlsid produces and verifies fibre gRPC TLS certificates that bind
// the ephemeral TLS keypair to a validator's consensus identity.
//
// A fibre server generates a fresh Ed25519 keypair on every start. The
// self-signed certificate it presents carries a custom X.509 extension with
// the layout (ASN.1 DER):
//
//	SignedIdentity ::= SEQUENCE {
//	    pubKey    OCTET STRING,  -- proto-encoded cometbft consensus pubkey
//	    signature OCTET STRING   -- consensus key signature
//	}
//
// The signature is produced by [core.PrivValidator.SignRawBytes] over
// [SignPrefix] concatenated with the DER-encoded TLS SubjectPublicKeyInfo,
// using [SignUniqueID] for domain separation. The fibre client supplies the
// expected consensus pubkey to [VerifyPeer] and rejects any peer whose
// certificate does not contain a matching, validly-signed extension.
//
// The design follows the libp2p-TLS spec
// (https://github.com/libp2p/specs/blob/master/tls/tls.md) so browser clients
// (e.g. Lumina) can interoperate with the same machinery.
package tlsid

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/encoding"
	cryptoproto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	core "github.com/cometbft/cometbft/types"
)

// SignUniqueID is the privval domain-separation tag used when binding the TLS
// keypair to a validator's consensus identity. Bumping this string is a
// protocol-level break.
const SignUniqueID = "celestia-fibre-tls-v1"

// SignPrefix is mixed into the signed payload alongside SignUniqueID so the
// signature can never collide with another payload signed by the same key.
const SignPrefix = "celestia-fibre-tls:"

// CertValidity is how long generated certificates remain valid. Certs are
// regenerated on every server start so this only bounds the upper edge.
const CertValidity = 365 * 24 * time.Hour

// signedIDExtensionOID identifies the custom certificate extension. Bumping
// this OID is a protocol-level break.
var signedIDExtensionOID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 56843, 1, 1}

// signedIdentity is the ASN.1 payload of the custom extension.
type signedIdentity struct {
	PubKey    []byte
	Signature []byte
}

// BuildServerCert generates an ephemeral Ed25519 TLS keypair and a self-signed
// certificate that binds it to the consensus identity exposed by signer.
// The signer is invoked once via SignRawBytes; chainID provides domain
// separation against other chains using the same key.
func BuildServerCert(signer core.PrivValidator, chainID string) (tls.Certificate, error) {
	if signer == nil {
		return tls.Certificate{}, errors.New("signer must not be nil")
	}
	if chainID == "" {
		return tls.Certificate{}, errors.New("chainID must not be empty")
	}

	tlsPub, tlsPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate TLS keypair: %w", err)
	}

	tlsPubDER, err := x509.MarshalPKIXPublicKey(tlsPub)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal TLS pubkey: %w", err)
	}

	payload := signedPayload(tlsPubDER)
	sig, err := signer.SignRawBytes(chainID, SignUniqueID, payload)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("sign TLS binding: %w", err)
	}

	consPub, err := signer.GetPubKey()
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("get consensus pubkey: %w", err)
	}
	consPubBytes, err := marshalConsPubKey(consPub)
	if err != nil {
		return tls.Certificate{}, err
	}

	extBytes, err := asn1.Marshal(signedIdentity{PubKey: consPubBytes, Signature: sig})
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal identity extension: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate cert serial: %w", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "celestia-fibre"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(CertValidity),
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

// VerifyPeer returns a [tls.Config.VerifyPeerCertificate] callback that
// accepts a peer iff its leaf certificate carries a validly-signed identity
// extension whose embedded consensus pubkey equals expected. Use only with
// tls.Config.InsecureSkipVerify=true; we deliberately bypass CA/hostname
// validation in favor of identity binding.
//
// chainID must match the chainID the server used when signing the extension,
// otherwise the signature will not verify.
func VerifyPeer(expected crypto.PubKey, chainID string) func([][]byte, [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if expected == nil {
			return errors.New("no expected validator pubkey")
		}
		if len(rawCerts) == 0 {
			return errors.New("peer presented no certificate")
		}
		cert, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("parse peer cert: %w", err)
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

		var id signedIdentity
		rest, err := asn1.Unmarshal(extBytes, &id)
		if err != nil {
			return fmt.Errorf("unmarshal identity extension: %w", err)
		}
		if len(rest) != 0 {
			return errors.New("trailing bytes in identity extension")
		}

		peerPub, err := unmarshalConsPubKey(id.PubKey)
		if err != nil {
			return err
		}
		if !peerPub.Equals(expected) {
			return fmt.Errorf("peer identity mismatch: expected %X got %X",
				expected.Address(), peerPub.Address())
		}

		tlsPubDER, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
		if err != nil {
			return fmt.Errorf("marshal peer TLS pubkey: %w", err)
		}
		signed, err := core.RawBytesMessageSignBytes(chainID, SignUniqueID, signedPayload(tlsPubDER))
		if err != nil {
			return fmt.Errorf("compute signed bytes: %w", err)
		}
		if !peerPub.VerifySignature(signed, id.Signature) {
			return errors.New("peer cert signature is invalid")
		}
		return nil
	}
}

func signedPayload(tlsPubDER []byte) []byte {
	out := make([]byte, 0, len(SignPrefix)+len(tlsPubDER))
	out = append(out, SignPrefix...)
	out = append(out, tlsPubDER...)
	return out
}

func marshalConsPubKey(pk crypto.PubKey) ([]byte, error) {
	proto, err := encoding.PubKeyToProto(pk)
	if err != nil {
		return nil, fmt.Errorf("encode consensus pubkey to proto: %w", err)
	}
	b, err := proto.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshal consensus pubkey proto: %w", err)
	}
	return b, nil
}

func unmarshalConsPubKey(b []byte) (crypto.PubKey, error) {
	var pkProto cryptoproto.PublicKey
	if err := pkProto.Unmarshal(b); err != nil {
		return nil, fmt.Errorf("unmarshal consensus pubkey proto: %w", err)
	}
	pk, err := encoding.PubKeyFromProto(pkProto)
	if err != nil {
		return nil, fmt.Errorf("decode consensus pubkey: %w", err)
	}
	return pk, nil
}
