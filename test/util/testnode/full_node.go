package testnode

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	pruningtypes "github.com/cosmos/cosmos-sdk/pruning/types"
	"github.com/cosmos/cosmos-sdk/server"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	srvtypes "github.com/cosmos/cosmos-sdk/server/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/log"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/types"
	dbm "github.com/tendermint/tm-db"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/cmd/celestia-appd/cmd"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	qgbtypes "github.com/celestiaorg/celestia-app/x/qgb/types"
)

// New creates a ready to use tendermint node that operates a single validator
// celestia-app network using the provided genesis state. The provided keyring
// is stored in the client.Context that is returned.
//
// NOTE: the forced delay between blocks, TimeIotaMs in the consensus
// parameters, is set to the lowest possible value (1ms).
func New(
	t testing.TB,
	cparams *tmproto.ConsensusParams,
	tmCfg *config.Config,
	supressLog bool,
	genState map[string]json.RawMessage,
	kr keyring.Keyring,
	chainID string,
) (*node.Node, srvtypes.Application, Context, error) {
	var logger log.Logger
	if supressLog {
		logger = log.NewNopLogger()
	} else {
		logger = log.NewTMLogger(log.NewSyncWriter(os.Stdout))
		logger = log.NewFilter(logger, log.AllowError())
	}

	baseDir, err := initFileStructure(t, tmCfg)
	if err != nil {
		return nil, nil, Context{}, err
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	nodeKey, err := p2p.LoadOrGenNodeKey(tmCfg.NodeKeyFile())
	if err != nil {
		return nil, nil, Context{}, err
	}

	nodeID, pubKey, err := genutil.InitializeNodeValidatorFiles(tmCfg)
	if err != nil {
		return nil, nil, Context{}, err
	}

	err = createValidator(kr, encCfg, pubKey, "validator", nodeID, chainID, baseDir)
	if err != nil {
		return nil, nil, Context{}, err
	}

	err = initGenFiles(cparams, genState, encCfg.Codec, tmCfg.GenesisFile(), chainID)
	if err != nil {
		return nil, nil, Context{}, err
	}

	err = collectGenFiles(tmCfg, encCfg, pubKey, nodeID, chainID, baseDir)
	if err != nil {
		return nil, nil, Context{}, err
	}

	db := dbm.NewMemDB()

	appOpts := appOptions{
		options: map[string]interface{}{
			server.FlagPruning: pruningtypes.PruningOptionNothing,
			flags.FlagHome:     baseDir,
		},
	}

	app := cmd.NewAppServer(logger, db, nil, appOpts)

	tmNode, err := node.NewNode(
		tmCfg,
		privval.LoadOrGenFilePV(tmCfg.PrivValidatorKeyFile(), tmCfg.PrivValidatorStateFile()),
		nodeKey,
		proxy.NewLocalClientCreator(app),
		node.DefaultGenesisDocProviderFunc(tmCfg),
		node.DefaultDBProvider,
		node.DefaultMetricsProvider(tmCfg.Instrumentation),
		logger,
	)

	cCtx := Context{}.
		WithKeyring(kr).
		WithHomeDir(tmCfg.RootDir).
		WithChainID(chainID).
		WithInterfaceRegistry(encCfg.InterfaceRegistry).
		WithCodec(encCfg.Codec).
		WithLegacyAmino(encCfg.Amino).
		WithTxConfig(encCfg.TxConfig).
		WithAccountRetriever(authtypes.AccountRetriever{})

	return tmNode, app, Context{Context: cCtx}, err
}

type appOptions struct {
	options map[string]interface{}
}

// Get implements AppOptions
func (ao appOptions) Get(o string) interface{} {
	return ao.options[o]
}

func DefaultParams() *tmproto.ConsensusParams {
	cparams := types.DefaultConsensusParams()
	cparams.Block.TimeIotaMs = 1
	return cparams
}

func DefaultTendermintConfig() *config.Config {
	tmCfg := config.DefaultConfig()
	// Reduce the target height duration so that blocks are produced faster
	// during tests.
	tmCfg.Consensus.TargetHeightDuration = 300 * time.Millisecond
	tmCfg.Consensus.TimeoutPropose = 200 * time.Millisecond
	tmCfg.Mempool.MaxTxBytes = 22020096 // 21MB
	return tmCfg
}

// DefaultGenesisState returns a default genesis state and a keyring with
// accounts that have coins. The keyring accounts are based on the
// fundedAccounts parameter.
func DefaultGenesisState(fundedAccounts ...string) (map[string]json.RawMessage, keyring.Keyring, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	state := app.ModuleBasics.DefaultGenesis(encCfg.Codec)
	fundedAccounts = append(fundedAccounts, "validator")
	kr, bankBals, authAccs := testfactory.FundKeyringAccounts(fundedAccounts...)

	// set the accounts in the genesis state
	var authGenState authtypes.GenesisState
	encCfg.Codec.MustUnmarshalJSON(state[authtypes.ModuleName], &authGenState)

	accounts, err := authtypes.PackAccounts(authAccs)
	if err != nil {
		return nil, nil, err
	}

	authGenState.Accounts = append(authGenState.Accounts, accounts...)
	state[authtypes.ModuleName] = encCfg.Codec.MustMarshalJSON(&authGenState)

	// set the balances in the genesis state
	var bankGenState banktypes.GenesisState
	encCfg.Codec.MustUnmarshalJSON(state[banktypes.ModuleName], &bankGenState)

	bankGenState.Balances = append(bankGenState.Balances, bankBals...)
	state[banktypes.ModuleName] = encCfg.Codec.MustMarshalJSON(&bankGenState)

	// use the minimum data commitment window (100)
	var qgbGenState qgbtypes.GenesisState
	encCfg.Codec.MustUnmarshalJSON(state[qgbtypes.ModuleName], &qgbGenState)
	qgbGenState.Params.DataCommitmentWindow = qgbtypes.MinimumDataCommitmentWindow
	state[qgbtypes.ModuleName] = encCfg.Codec.MustMarshalJSON(&qgbGenState)

	return state, kr, nil
}

// NewNetwork starts a single valiator celestia-app network using the provided
// configurations. Provided accounts will be funded and their keys can be
// accessed in keyring returned client.Context. All rpc, p2p, and grpc addresses
// in the provided configs are overwritten to use open ports. The node can be
// accessed via the returned client.Context or via the returned rpc and grpc
// addresses.
func NewNetwork(
	t testing.TB,
	cparams *tmproto.ConsensusParams,
	tmCfg *config.Config,
	appCfg *srvconfig.Config,
	accounts ...string,
) (cctx Context, rpcAddr, grpcAddr string) {
	t.Helper()

	tmCfg.RPC.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", GetFreePort())
	tmCfg.P2P.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", GetFreePort())
	tmCfg.RPC.GRPCListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", GetFreePort())

	genState, kr, err := DefaultGenesisState(accounts...)
	require.NoError(t, err)

	tmNode, app, cctx, err := New(t, cparams, tmCfg, false, genState, kr, tmrand.Str(6))
	require.NoError(t, err)

	cctx, stopNode, err := StartNode(tmNode, cctx)
	require.NoError(t, err)

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

// DefaultNetwork starts a single valiator celestia-app network using test
// friendly defaults. These defaults include all default values and genesis
// params, modified for fast block times and 300 funded accounts. The returned
// client.Context has a keyring with all of the funded keys stored in it, which
// can be accessed using the returned accounts.
func DefaultNetwork(t *testing.T) (accounts []string, cctx Context) {
	// we create an arbitrary number of funded accounts
	accounts = make([]string, 300)
	for i := 0; i < 300; i++ {
		accounts[i] = tmrand.Str(9)
	}

	tmCfg := DefaultTendermintConfig()
	tmCfg.Consensus.TargetHeightDuration = time.Millisecond * 1
	appConf := DefaultAppConfig()

	cctx, _, _ = NewNetwork(t, DefaultParams(), tmCfg, appConf, accounts...)

	return accounts, cctx
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
