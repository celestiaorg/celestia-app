package testnode

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/celestiaorg/celestia-app/test/util/genesis"
	"github.com/cosmos/cosmos-sdk/client/flags"
	srvtypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
	dbm "github.com/tendermint/tm-db"
)

// NewCometNode creates a ready to use comet node that operates a single
// validator celestia-app network. It expects that all configuration files are
// already initialized and saved to the baseDir.
func NewCometNode(t testing.TB, baseDir string, cfg *Config) (*node.Node, srvtypes.Application, error) {
	var logger log.Logger
	if cfg.SupressLogs {
		logger = log.NewNopLogger()
	} else {
		logger = log.NewTMLogger(log.NewSyncWriter(os.Stdout))
		logger = log.NewFilter(logger, log.AllowError())
	}

	dbPath := filepath.Join(cfg.TmConfig.RootDir, "data")
	db, err := dbm.NewGoLevelDB("application", dbPath)
	require.NoError(t, err)

	cfg.AppOptions.Set(flags.FlagHome, baseDir)

	app := cfg.AppCreator(logger, db, nil, cfg.AppOptions)

	nodeKey, err := p2p.LoadOrGenNodeKey(cfg.TmConfig.NodeKeyFile())
	if err != nil {
		return nil, nil, err
	}

	tmNode, err := node.NewNode(
		cfg.TmConfig,
		privval.LoadOrGenFilePV(cfg.TmConfig.PrivValidatorKeyFile(), cfg.TmConfig.PrivValidatorStateFile()),
		nodeKey,
		proxy.NewLocalClientCreator(app),
		node.DefaultGenesisDocProviderFunc(cfg.TmConfig),
		node.DefaultDBProvider,
		node.DefaultMetricsProvider(cfg.TmConfig.Instrumentation),
		logger,
	)

	return tmNode, app, err
}

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
	tmCfg.RPC.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", GetFreePort())
	tmCfg.P2P.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", GetFreePort())
	tmCfg.RPC.GRPCListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", GetFreePort())

	// initialize the genesis file and validator files for the first validator.
	baseDir, err := genesis.InitFiles(t.TempDir(), tmCfg, cfg.Genesis, 0)
	require.NoError(t, err)

	tmNode, app, err := NewCometNode(t, baseDir, cfg)
	require.NoError(t, err)

	cctx = NewContext(context.TODO(), cfg.Genesis.Keyring(), tmCfg, cfg.Genesis.ChainID)

	cctx, stopNode, err := StartNode(tmNode, cctx)
	require.NoError(t, err)

	appCfg := cfg.AppConfig
	appCfg.GRPC.Address = fmt.Sprintf("127.0.0.1:%d", GetFreePort())
	appCfg.API.Address = fmt.Sprintf("tcp://127.0.0.1:%d", GetFreePort())

	cctx, cleanupGRPC, err := StartGRPCServer(app, appCfg, cctx)
	require.NoError(t, err)

	t.Cleanup(func() {
		t.Log("tearing down testnode")
		require.NoError(t, stopNode())
		require.NoError(t, cleanupGRPC())
	})

	return cctx, tmCfg.RPC.ListenAddress, appCfg.GRPC.Address
}

func GetFreePort() int {
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
