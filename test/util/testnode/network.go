package testnode

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/celestiaorg/celestia-app/test/util/genesis"
	"github.com/stretchr/testify/require"
)

// NewNetwork starts a single valiator celestia-app network using the provided
// configurations. Configured accounts will be funded and their keys can be
// accessed in keyring returned client.Context. All rpc, p2p, and grpc addresses
// in the provided configs are overwritten to use open ports. The node can be
// accessed via the returned client.Context or via the returned rpc and grpc
// addresses. Configured genesis options will be applied after all accounts have
// been initialized.
func NewNetwork(t testing.TB, cfg *Config) (cctx Context, rpcAddr, grpcAddr string) {
	t.Helper()

	tmCfg := cfg.TmConfig
	tmCfg.RPC.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", getFreePort())
	tmCfg.P2P.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", getFreePort())
	tmCfg.RPC.GRPCListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", getFreePort())

	// initialize the genesis file and validator files for the first validator.
	baseDir, err := genesis.InitFiles(t.TempDir(), tmCfg, cfg.Genesis, 0)
	require.NoError(t, err)

	tmNode, app, err := NewCometNode(t, baseDir, cfg)
	require.NoError(t, err)

	cctx = NewContext(context.Background(), cfg.Genesis.Keyring(), tmCfg, cfg.Genesis.ChainID)

	cctx, stopNode, err := StartNode(t, tmNode, cctx)
	require.NoError(t, err)

	appCfg := cfg.AppConfig
	appCfg.GRPC.Address = fmt.Sprintf("127.0.0.1:%d", getFreePort())
	appCfg.API.Address = fmt.Sprintf("tcp://127.0.0.1:%d", getFreePort())

	cctx, cleanupGRPC, err := StartGRPCServer(app, appCfg, cctx)
	require.NoError(t, err)

	t.Cleanup(func() {
		t.Log("tearing down testnode")
		require.NoError(t, stopNode())
		require.NoError(t, cleanupGRPC())
	})

	return cctx, tmCfg.RPC.ListenAddress, appCfg.GRPC.Address
}

func getFreePort() int {
	a, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close()
			return l.Addr().(*net.TCPAddr).Port
		}
	}
	panic("while getting free port: " + err.Error())
}
