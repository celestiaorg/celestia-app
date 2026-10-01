package sign_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre/internal/sign"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/privval"
	privvalproto "github.com/cometbft/cometbft/proto/tendermint/privval"
	"github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// testCerts holds PEM file paths for an ephemeral CA, server cert, and client cert.
type testCerts struct {
	caFile, serverCertFile, serverKeyFile, clientCertFile, clientKeyFile string
}

// generateTestCerts creates a CA plus CA-signed server and client certificates
// and writes them as PEM files under t.TempDir().
func generateTestCerts(t *testing.T) testCerts {
	t.Helper()
	dir := t.TempDir()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	newCert := func(name string, serial int64, extUsage x509.ExtKeyUsage) (certPEM, keyPEM []byte) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(serial),
			Subject:      pkix.Name{CommonName: name},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{extUsage},
			IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, caTmpl, &key.PublicKey, caKey)
		require.NoError(t, err)
		keyDER, err := x509.MarshalECPrivateKey(key)
		require.NoError(t, err)
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	}

	serverCert, serverKey := newCert("test-server", 2, x509.ExtKeyUsageServerAuth)
	clientCert, clientKey := newCert("test-client", 3, x509.ExtKeyUsageClientAuth)

	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, data, 0o600))
		return path
	}

	return testCerts{
		caFile:         write("ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})),
		serverCertFile: write("server-cert.pem", serverCert),
		serverKeyFile:  write("server-key.pem", serverKey),
		clientCertFile: write("client-cert.pem", clientCert),
		clientKeyFile:  write("client-key.pem", clientKey),
	}
}

// startTLSTestServer starts a PrivValidatorAPI gRPC server requiring client
// certificates and returns its address.
func startTLSTestServer(t *testing.T, pv types.PrivValidator, certs testCerts) string {
	t.Helper()

	serverCert, err := tls.LoadX509KeyPair(certs.serverCertFile, certs.serverKeyFile)
	require.NoError(t, err)
	caPEM, err := os.ReadFile(certs.caFile)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(caPEM))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	})))
	privvalproto.RegisterPrivValidatorAPIServer(srv, privval.NewPrivValidatorGRPCServer(
		pv,
		log.NewNopLogger(),
	))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return lis.Addr().String()
}

func TestGRPCClientMutualTLS(t *testing.T) {
	certs := generateTestCerts(t)
	pv := types.NewMockPV()
	addr := startTLSTestServer(t, pv, certs)

	client, err := sign.NewGRPCClient(addr, testChainID, &sign.TLSConfig{
		CAFile:   certs.caFile,
		CertFile: certs.clientCertFile,
		KeyFile:  certs.clientKeyFile,
	}, slog.Default())
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	expectedPubKey, err := pv.GetPubKey()
	require.NoError(t, err)

	got, err := client.GetPubKey()
	require.NoError(t, err)
	assert.Equal(t, expectedPubKey, got)
}

func TestGRPCClientMutualTLSMissingClientCert(t *testing.T) {
	certs := generateTestCerts(t)

	_, err := sign.NewGRPCClient("127.0.0.1:0", testChainID, &sign.TLSConfig{
		CAFile: certs.caFile,
	}, slog.Default())
	require.Error(t, err)
}
