package testnode

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/cmd/celestia-appd/cmd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	pruningtypes "github.com/cosmos/cosmos-sdk/pruning/types"
	"github.com/cosmos/cosmos-sdk/server"
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
)

// New creates a ready to use tendermint node that operates a single validator
// celestia-app network using the provided genesis state. The provided keyring
// is stored in the client.Context that is returned.
//
// NOTE: the forced delay between blocks, TimeIotaMs in the consensus
// parameters, is set to the lowest possible value (1ms).
func New(
	t *testing.T,
	cparams *tmproto.ConsensusParams,
	tmCfg *config.Config,
	supressLog bool,
	genState map[string]json.RawMessage,
	kr keyring.Keyring,
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

	chainID := tmrand.Str(6)

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
	tmCfg.Consensus.TimeoutCommit = time.Millisecond * 200
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
	kr, bankBals, authAccs := fundKeyringAccounts(encCfg.Codec, fundedAccounts...)

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

	return state, kr, nil
}

// DefaultNetwork creates an in-process single validator celestia-app network
// using test friendly defaults. These defaults include fast block times and
// funded accounts. The returned client.Context has a keyring with all of the
// funded keys stored in it.
func DefaultNetwork(t *testing.T, blockTime time.Duration) (cleanup func() error, accounts []string, cctx Context) {
	// we create an arbitrary number of funded accounts
	accounts = make([]string, 300)
	for i := 0; i < 300; i++ {
		accounts[i] = tmrand.Str(9)
	}

	tmCfg := DefaultTendermintConfig()
	tmCfg.Consensus.TimeoutCommit = blockTime

	genState, kr, err := DefaultGenesisState(accounts...)
	require.NoError(t, err)

	tmNode, app, cctx, err := New(t, DefaultParams(), tmCfg, false, genState, kr)
	require.NoError(t, err)

	cctx, stopNode, err := StartNode(tmNode, cctx)
	require.NoError(t, err)

	cctx, cleanupGRPC, err := StartGRPCServer(app, DefaultAppConfig(), cctx)
	require.NoError(t, err)

	return func() error {
		err := stopNode()
		if err != nil {
			return err
		}
		return cleanupGRPC()

	}, accounts, cctx
}
