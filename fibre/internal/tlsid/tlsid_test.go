package tlsid_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/fibre/internal/tlsid"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testChainID = "test-chain"

func TestBuildAndVerify_RoundTrip(t *testing.T) {
	pv := core.NewMockPV()
	cert, err := tlsid.BuildServerCert(pv, testChainID)
	require.NoError(t, err)
	require.NotNil(t, cert.PrivateKey)
	require.Len(t, cert.Certificate, 1)

	expectedPub, err := pv.GetPubKey()
	require.NoError(t, err)

	verify := tlsid.VerifyPeer(expectedPub, testChainID)
	require.NoError(t, verify(cert.Certificate, nil))
}

func TestVerify_RejectsWrongValidator(t *testing.T) {
	server := core.NewMockPV()
	cert, err := tlsid.BuildServerCert(server, testChainID)
	require.NoError(t, err)

	other := core.NewMockPV()
	otherPub, err := other.GetPubKey()
	require.NoError(t, err)

	// The endorsement was signed by `server`, so verifying against `other`'s
	// pubkey fails the signature check (identity is established by the signature
	// verifying under the expected key, not by an embedded pubkey field).
	err = tlsid.VerifyPeer(otherPub, testChainID)(cert.Certificate, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature is invalid")
}

func TestVerify_RejectsWrongChainID(t *testing.T) {
	pv := core.NewMockPV()
	cert, err := tlsid.BuildServerCert(pv, testChainID)
	require.NoError(t, err)

	pub, err := pv.GetPubKey()
	require.NoError(t, err)

	err = tlsid.VerifyPeer(pub, "other-chain")(cert.Certificate, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature is invalid")
}

func TestBuildServerCert_UsesProvidedChainID(t *testing.T) {
	pv := &chainIDEnforcingPV{
		PrivValidator: core.NewMockPV(),
		chainID:       testChainID,
	}

	_, err := tlsid.BuildServerCert(pv, testChainID)
	require.NoError(t, err)

	_, err = tlsid.BuildServerCert(pv, "other-chain")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected chain ID")
}

func TestVerify_RejectsMissingExtension(t *testing.T) {
	// Build a self-signed cert with no identity extension.
	rawCert := buildPlainSelfSignedCert(t)

	pv := core.NewMockPV()
	pub, err := pv.GetPubKey()
	require.NoError(t, err)

	err = tlsid.VerifyPeer(pub, testChainID)([][]byte{rawCert}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing the fibre identity extension")
}

func TestVerify_RejectsEmptyChain(t *testing.T) {
	pv := core.NewMockPV()
	pub, err := pv.GetPubKey()
	require.NoError(t, err)

	err = tlsid.VerifyPeer(pub, testChainID)(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no certificate")
}

func TestVerify_RejectsNilExpected(t *testing.T) {
	pv := core.NewMockPV()
	cert, err := tlsid.BuildServerCert(pv, testChainID)
	require.NoError(t, err)

	err = tlsid.VerifyPeer(nil, testChainID)(cert.Certificate, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no expected validator pubkey")
}

func TestVerify_RejectsTamperedSignature(t *testing.T) {
	pv := core.NewMockPV()
	cert, err := tlsid.BuildServerCert(pv, testChainID)
	require.NoError(t, err)

	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)

	// Locate the extension and flip a bit inside its signature octet string.
	tampered := tamperIdentityExtension(t, parsed)

	pub, err := pv.GetPubKey()
	require.NoError(t, err)

	err = tlsid.VerifyPeer(pub, testChainID)([][]byte{tampered}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature is invalid")
}

func TestBuildServerCert_RejectsNilSigner(t *testing.T) {
	_, err := tlsid.BuildServerCert(nil, testChainID)
	require.Error(t, err)
}

func TestBuildServerCert_RejectsEmptyChainID(t *testing.T) {
	_, err := tlsid.BuildServerCert(core.NewMockPV(), "")
	require.Error(t, err)
}

func TestTLSHandshake_EndToEnd(t *testing.T) {
	pv := core.NewMockPV()
	cert, err := tlsid.BuildServerCert(pv, testChainID)
	require.NoError(t, err)

	serverCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	pub, err := pv.GetPubKey()
	require.NoError(t, err)
	clientCfg := &tls.Config{
		InsecureSkipVerify:    true, //nolint:gosec // identity is checked via VerifyPeerCertificate
		VerifyPeerCertificate: tlsid.VerifyPeer(pub, testChainID),
		MinVersion:            tls.VersionTLS13,
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	srvDone := make(chan error, 1)
	go func() {
		raw, acceptErr := ln.Accept()
		if acceptErr != nil {
			srvDone <- acceptErr
			return
		}
		srvDone <- tls.Server(raw, serverCfg).Handshake()
	}()

	rawClient, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer rawClient.Close()

	tlsClient := tls.Client(rawClient, clientCfg)
	require.NoError(t, tlsClient.Handshake())
	require.NoError(t, <-srvDone)
}

type chainIDEnforcingPV struct {
	core.PrivValidator
	chainID string
}

func (pv *chainIDEnforcingPV) SignRawBytes(chainID, uniqueID string, rawBytes []byte) ([]byte, error) {
	if chainID != pv.chainID {
		return nil, fmt.Errorf("unexpected chain ID %q", chainID)
	}
	return pv.PrivValidator.SignRawBytes(chainID, uniqueID, rawBytes)
}

// buildPlainSelfSignedCert returns DER bytes of a self-signed Ed25519 cert
// with no custom extension.
func buildPlainSelfSignedCert(t *testing.T) []byte {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "no-extension"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	require.NoError(t, err)
	return der
}

// tamperIdentityExtension rebuilds a fresh self-signed cert that carries an
// extension whose signature octet string has been bit-flipped. We can't simply
// edit the original DER because x509.CreateCertificate is the only path that
// produces a valid DER body without a custom encoder.
func tamperIdentityExtension(t *testing.T, src *x509.Certificate) []byte {
	t.Helper()
	var ext pkix.Extension
	for _, e := range src.Extensions {
		if e.Id.Equal(asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 32473, 1, 1}) {
			ext = e
			break
		}
	}
	require.NotEmpty(t, ext.Value)

	type signedIdentity struct {
		Payload   []byte
		Signature []byte
	}
	var id signedIdentity
	_, err := asn1.Unmarshal(ext.Value, &id)
	require.NoError(t, err)
	require.NotEmpty(t, id.Signature)
	id.Signature[0] ^= 0xFF

	repacked, err := asn1.Marshal(id)
	require.NoError(t, err)

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "tampered"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		ExtraExtensions: []pkix.Extension{{
			Id:    ext.Id,
			Value: repacked,
		}},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	require.NoError(t, err)
	return der
}
