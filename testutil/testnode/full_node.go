package testnode

import (
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/cmd/celestia-appd/cmd"
	pruningtypes "github.com/cosmos/cosmos-sdk/pruning/types"
	"github.com/cosmos/cosmos-sdk/server"
	srvtypes "github.com/cosmos/cosmos-sdk/server/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
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
// celestia-app network. The passed account names in fundedAccounts are used to
// generate new private keys, which are included as funded accounts in the
// genesis file. These keys are stored in the keyring that is returned in the
// client.Context. NOTE: the forced delay between blocks, TimeIotaMs in the
// consensus parameters, is set to the lowest possible value (1ms).
func New(t *testing.T, cparams *tmproto.ConsensusParams, tmCfg *config.Config, supressLog bool, fundedAccounts ...string) (*node.Node, srvtypes.Application, Context, error) {
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

	genState := app.ModuleBasics.DefaultGenesis(encCfg.Codec)

	fundedAccounts = append(fundedAccounts, "validator")

	kr, bankBals, authAccs := fundKeyringAccounts(encCfg.Codec, fundedAccounts...)

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

	err = initGenFiles(cparams, genState, encCfg.Codec, authAccs, bankBals, tmCfg.GenesisFile(), chainID)
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
	tmCfg.Consensus.TimeoutCommit = time.Millisecond * 100
	tmCfg.Mempool.MaxTxBytes = 22020096 // 21MB
	return tmCfg
}
