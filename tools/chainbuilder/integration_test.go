package main

import (
	"context"
	"fmt"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/baseapp"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/rpc/client/local"
	tmdbm "github.com/tendermint/tm-db"

	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chainbuilder tool test")
	}

	numBlocks := 10

	cfg := BuilderConfig{
		NumBlocks:     numBlocks,
		BlockSize:     appconsts.DefaultMaxBytes,
		BlockInterval: time.Second,
		ChainID:       tmrand.Str(6),
		Namespace:     defaultNamespace,
	}

	dir := t.TempDir()

	// First run
	err := Run(context.Background(), cfg, dir)
	require.NoError(t, err)

	// Second run with existing directory
	cfg.ExistingDir = filepath.Join(dir, fmt.Sprintf("testnode-%s", cfg.ChainID))
	err = Run(context.Background(), cfg, dir)
	require.NoError(t, err)

	tmCfg := testnode.DefaultTendermintConfig()
	tmCfg.SetRoot(cfg.ExistingDir)

	appDB, err := tmdbm.NewDB("application", tmdbm.GoLevelDBBackend, tmCfg.DBDir())
	require.NoError(t, err)

	encCfg := encoding.MakeConfig(app.ModuleBasics)

	testApp := app.New(
		log.NewNopLogger(),
		appDB,
		nil,
		0,
		encCfg,
		0, // upgrade height v2
		0, // timeout commit
		util.EmptyAppOptions{},
		baseapp.SetMinGasPrices(fmt.Sprintf("%f%s", appconsts.DefaultMinGasPrice, appconsts.BondDenom)),
	)

	nodeKey, err := p2p.LoadNodeKey(tmCfg.NodeKeyFile())
	require.NoError(t, err)

	genProvider := node.DefaultGenesisDocProviderFunc(tmCfg)

	cometNode, err := node.NewNode(
		tmCfg,
		privval.LoadOrGenFilePV(tmCfg.PrivValidatorKeyFile(), tmCfg.PrivValidatorStateFile()),
		nodeKey,
		proxy.NewLocalClientCreator(testApp),
		genProvider,
		node.DefaultDBProvider,
		node.DefaultMetricsProvider(tmCfg.Instrumentation),
		log.NewNopLogger(),
	)
	require.NoError(t, err)

	require.NoError(t, cometNode.Start())
	defer func() { _ = cometNode.Stop() }()

	client := local.New(cometNode)
	status, err := client.Status(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status)
	// assert that the new node eventually makes progress in the chain
	require.Eventually(t, func() bool {
		status, err := client.Status(context.Background())
		require.NoError(t, err)
		return status.SyncInfo.LatestBlockHeight >= int64(numBlocks+2)
	}, time.Second*10, time.Millisecond*100)

	// third: require that another node can sync up on the same blockchain
	appDB2, err := tmdbm.NewDB("application2", tmdbm.GoLevelDBBackend, tmCfg.DBDir())
	require.NoError(t, err)

	testApp2 := app.New(
		log.NewTMJSONLogger(os.Stdout),
		appDB2,
		nil,
		0,
		encCfg,
		0, // upgrade height v2
		0, // timeout commit
		util.EmptyAppOptions{},
		baseapp.SetMinGasPrices(fmt.Sprintf("%f%s", appconsts.DefaultMinGasPrice, appconsts.BondDenom)),
	)

	gen, err := genProvider()
	require.NoError(t, err)

	abciParams := &abci.ConsensusParams{
		Block: &abci.BlockParams{
			MaxBytes: gen.ConsensusParams.Block.MaxBytes,
			MaxGas:   gen.ConsensusParams.Block.MaxGas,
		},
		Evidence:  &gen.ConsensusParams.Evidence,
		Validator: &gen.ConsensusParams.Validator,
		Version:   &gen.ConsensusParams.Version,
	}

	testApp2.InitChain(abci.RequestInitChain{
		Time:            gen.GenesisTime,
		ChainId:         gen.ChainID,
		Validators:      []abci.ValidatorUpdate{},
		ConsensusParams: abciParams,
		AppStateBytes:   gen.AppState,
	})

	testApp2.Info(proxy.RequestInfo)

	for height := int64(1); height <= int64(numBlocks); height++ {
		block, err := client.Block(context.Background(), &height)
		require.NoError(t, err)
		require.NotNil(t, block)

		h := block.Block.Header.ToProto()
		data := block.Block.Data.ToProto()

		resp := testApp2.ProcessProposal(abci.RequestProcessProposal{
			Header:    *h,
			BlockData: &data,
		})
		require.True(t, resp.IsOK(), height)

		testApp2.BeginBlock(abci.RequestBeginBlock{
			Header: *h,
		})

		for _, tx := range block.Block.Data.Txs {
			res := testApp2.DeliverTx(abci.RequestDeliverTx{
				Tx: tx,
			})
			require.True(t, res.IsOK())
		}

		testApp2.EndBlock(abci.RequestEndBlock{
			Height: height,
		})
		testApp2.Commit()
	}

	require.NoError(t, cometNode.Stop())
	cometNode.Wait()

}

func TestRunAuthParams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chainbuilder tool test")
	}

	numBlocks := 10

	cfg := BuilderConfig{
		NumBlocks:     numBlocks,
		BlockSize:     appconsts.DefaultMaxBytes,
		BlockInterval: time.Second,
		ChainID:       tmrand.Str(6),
		Namespace:     defaultNamespace,
	}

	dir := t.TempDir()

	// First run
	err := Run(context.Background(), cfg, dir)
	require.NoError(t, err)

	// Second run with existing directory
	cfg.ExistingDir = filepath.Join(dir, fmt.Sprintf("testnode-%s", cfg.ChainID))
	err = Run(context.Background(), cfg, dir)
	require.NoError(t, err)

	tmCfg := testnode.DefaultTendermintConfig()
	tmCfg.SetRoot(cfg.ExistingDir)

	appDB, err := tmdbm.NewDB("application", tmdbm.GoLevelDBBackend, tmCfg.DBDir())
	require.NoError(t, err)

	encCfg := encoding.MakeConfig(app.ModuleBasics)

	testApp := app.New(
		log.NewNopLogger(),
		appDB,
		nil,
		0,
		encCfg,
		0, // upgrade height v2
		0, // timeout commit
		util.EmptyAppOptions{},
		baseapp.SetMinGasPrices(fmt.Sprintf("%f%s", appconsts.DefaultMinGasPrice, appconsts.BondDenom)),
	)

	param := testApp.BlobKeeper.GetParams(testApp.NewContext(false, tmproto.Header{}))
	fmt.Println(param)
}
