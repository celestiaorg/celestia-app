package main

import (
	"context"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	dbm "github.com/cometbft/cometbft-db"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/store"
)

func main() {

}

func Run(ctx context.Context, numBlocks, blockSize int, blockInterval time.Duration) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	validator := genesis.NewDefaultValidator(testnode.DefaultValidatorAccountName)
	tmCfg := app.DefaultConsensusConfig()
	gen := genesis.NewDefaultGenesis().
		WithValidators(validator)

	if err := genesis.InitFiles(dir, tmCfg, gen, 0); err != nil {
		return err
	}

	key := privval.LoadFilePV(tmCfg.PrivValidatorKeyFile(), tmCfg.PrivValidatorStateFile())

	blockDb, err := dbm.NewDB("blockstore", dbm.GoLevelDBBackend, dir)
	if err != nil {
		return err
	}

	blockStore := store.NewBlockStore(blockDb)

	stateDb, err := dbm.NewDB("state", dbm.GoLevelDBBackend, dir)
	if err != nil {
		return err
	}

	stateStore := state.NewStore(stateDb, state.StoreOptions{
		DiscardABCIResponses: true,
	})

	lastHeight := blockStore.Height()

}
