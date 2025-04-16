package testnode

import (
	"context"
	"path/filepath"
	"testing"

	"cosmossdk.io/log"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/test/util/genesis"
)

// NewNetwork starts a single validator celestia-app network using the provided
// configurations. Configured accounts will be funded and their keys can be
// accessed in keyring returned client.Context. All rpc, p2p, and grpc addresses
// in the provided configs are overwritten to use open ports. The node can be
// accessed via the returned client.Context or via the returned rpc and grpc
// addresses. Configured genesis options will be applied after all accounts have
// been initialized.
func NewNetwork(t testing.TB, config *Config) (cctx Context, rpcAddr, grpcAddr string) {
	t.Helper()

	// initialize the genesis file and validator files for the first validator.
	baseDir := filepath.Join(t.TempDir(), "testnode")
	err := genesis.InitFiles(baseDir, config.TmConfig, config.AppConfig, config.Genesis, 0)
	require.NoError(t, err)

	tmNode, app, err := NewCometNode(baseDir, &config.UniversalTestingConfig)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cctx = NewContext(ctx, config.Genesis.Keyring(), config.TmConfig, config.Genesis.ChainID, config.AppConfig.API.Address)
	cctx.tmNode = tmNode

	cctx, stopNode, err := StartNode(tmNode, cctx)
	require.NoError(t, err)

	grpcServer, cctx, cleanupGRPC, err := StartGRPCServer(log.NewTestLogger(t), app, config.AppConfig, cctx)
	require.NoError(t, err)

	apiServer, err := StartAPIServer(app, *config.AppConfig, cctx, grpcServer)
	require.NoError(t, err)

	t.Cleanup(func() {
		t.Log("tearing down testnode")
		err := stopNode()
		if err != nil {
			// the test has already completed so log the error instead of
			// failing the test.
			t.Logf("error stopping node %v", err)
		}
		err = cleanupGRPC()
		if err != nil {
			// the test has already completed so just log the error instead of
			// failing the test.
			t.Logf("error when cleaning up GRPC %v", err)
		}
		err = apiServer.Close()
		if err != nil {
			// the test has already completed so just log the error instead of
			// failing the test.
			t.Logf("error when closing API server %v", err)
		}
	})

	return cctx, config.TmConfig.RPC.ListenAddress, config.AppConfig.GRPC.Address
}
