package utils

import (
	"context"

	"github.com/celestiaorg/celestia-app/v2/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
)

const baseDir = "~/.celestia-app-start-node"

func StartNode(cfg *testnode.Config) (cctx testnode.Context, err error) {
	// initialize the genesis file and validator files for the first validator.
	baseDir, err := genesis.InitFiles(baseDir, cfg.TmConfig, cfg.Genesis, 0)
	if err != nil {
		return testnode.Context{}, err
	}

	tmNode, _, err := testnode.NewCometNode(baseDir, &cfg.UniversalTestingConfig)
	if err != nil {
		return testnode.Context{}, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cctx = testnode.NewContext(ctx, cfg.Genesis.Keyring(), cfg.TmConfig, cfg.Genesis.ChainID, cfg.AppConfig.API.Address)

	cctx, stopNode, err := testnode.StartNode(tmNode, cctx)
	if err != nil {
		return testnode.Context{}, err
	}
	defer stopNode()

	return cctx, nil
}
