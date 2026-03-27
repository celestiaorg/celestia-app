package sign_test

import (
	"log/slog"
	"net"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/fibre/internal/sign"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/privval"
	privvalproto "github.com/cometbft/cometbft/proto/tendermint/privval"
	"github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

const testChainID = "test-chain"

// startTestServer spins up a gRPC server backed by the given PrivValidator
// and returns the listener address. The server is stopped on test cleanup.
func startTestServer(t *testing.T, pv types.PrivValidator) string {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := grpc.NewServer()
	privvalproto.RegisterPrivValidatorAPIServer(srv, privval.NewPrivValidatorGRPCServer(
		pv,
		log.NewNopLogger(),
	))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return lis.Addr().String()
}

func TestGRPCClientSignRawBytes(t *testing.T) {
	pv := types.NewMockPV()
	addr := startTestServer(t, pv)

	client, err := sign.NewGRPCClient(addr, testChainID, slog.Default())
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	rawBytes := []byte("test commitment data")
	uniqueID := "fiber-commitment"

	sig, err := client.SignRawBytes(testChainID, uniqueID, rawBytes)
	require.NoError(t, err)
	assert.NotEmpty(t, sig)

	// Verify the signature matches what the privval produces directly.
	expectedSig, err := pv.SignRawBytes(testChainID, uniqueID, rawBytes)
	require.NoError(t, err)
	assert.Equal(t, expectedSig, sig)
}

func TestGRPCClientGetPubKey(t *testing.T) {
	pv := types.NewMockPV()
	addr := startTestServer(t, pv)

	expectedPubKey, err := pv.GetPubKey()
	require.NoError(t, err)

	client, err := sign.NewGRPCClient(addr, testChainID, slog.Default())
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	got, err := client.GetPubKey()
	require.NoError(t, err)
	assert.Equal(t, expectedPubKey, got)
}

func TestGRPCClientSignRawBytesError(t *testing.T) {
	pv := types.NewErroringMockPV()
	addr := startTestServer(t, pv)

	client, err := sign.NewGRPCClient(addr, testChainID, slog.Default())
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.SignRawBytes(testChainID, "fiber-commitment", []byte("test data"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remote signer error")
}

func TestGRPCClientClose(t *testing.T) {
	pv := types.NewMockPV()
	addr := startTestServer(t, pv)

	client, err := sign.NewGRPCClient(addr, testChainID, slog.Default())
	require.NoError(t, err)

	require.NoError(t, client.Close())

	// After close, signing should fail.
	_, err = client.SignRawBytes(testChainID, "fiber-commitment", []byte("test"))
	require.Error(t, err)
}
