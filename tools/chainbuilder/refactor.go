package main

import (
	"context"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/tendermint/tendermint/node"
)

type ChainBuilderApp struct {
	stopNode func() error
	tmNode   *node.Node
	app      *app.App
}

func (cb *ChainBuilderApp) Start(ctx context.Context, baseDir string, cfg testnode.Config) error {
	err := genesis.InitFiles(baseDir, cfg.TmConfig, cfg.AppConfig, cfg.Genesis, 0)
	if err != nil {
		return err
	}

	tmNode, sapp, err := testnode.NewCometNode(baseDir, &cfg.UniversalTestingConfig)
	if err != nil {
		return err
	}

	cctx := testnode.NewContext(ctx, cfg.Genesis.Keyring(), cfg.TmConfig, cfg.Genesis.ChainID, cfg.AppConfig.API.Address)

	cctx, stopNode, err := testnode.StartNode(tmNode, cctx)
	if err != nil {
		return err
	}

	cb.stopNode = stopNode
	cb.tmNode = tmNode
	cb.app = sapp.(*app.App)

	return nil
}
