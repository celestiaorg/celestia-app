package cmd

import (
	"fmt"
	"github.com/01builders/nova/abci"
	"github.com/01builders/nova/internal"
	dbm "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/state"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/server/types"
)

func start(versions abci.Versions, svrCtx *server.Context, clientCtx client.Context, appCreator types.AppCreator) error {
	svrCfg, err := getAndValidateConfig(svrCtx)
	if err != nil {
		return err
	}

	state, err := getState(svrCtx.Config)
	if err != nil {
		return fmt.Errorf("failed to get current app state: %w", err)
	}

	appVersion := state.Version.Consensus.App

	svrCtx.Logger.Info("initializing multiplexer", "app_version", appVersion, "chain_id", state.ChainID)

	mp, err := abci.NewMultiplexer(svrCtx, svrCfg, clientCtx, appCreator, versions, state.ChainID, appVersion)
	if err != nil {
		return err
	}

	defer func() {
		if err := mp.Cleanup(); err != nil {
			svrCtx.Logger.Error("failed to cleanup multiplexer", "err", err)
		}
	}()

	// Start will either start the latest app natively, or an embedded app if one is specified.
	if err := mp.Start(); err != nil {
		return fmt.Errorf("failed to start multiplexer: %w", err)
	}

	return nil
}

// getState opens the db and fetches the existing state.
func getState(cfg *cmtcfg.Config) (state.State, error) {
	db, err := openDBM(cfg)
	if err != nil {
		return state.State{}, err
	}
	defer db.Close()

	s, _, err := node.LoadStateFromDBOrGenesisDocProvider(db, internal.GetGenDocProvider(cfg))
	if err != nil {
		return state.State{}, err
	}

	return s, nil
}

func getAndValidateConfig(svrCtx *server.Context) (serverconfig.Config, error) {
	config, err := serverconfig.GetConfig(svrCtx.Viper)
	if err != nil {
		return config, err
	}

	if err := config.ValidateBasic(); err != nil {
		return config, err
	}
	return config, nil
}

func openDBM(cfg *cmtcfg.Config) (dbm.DB, error) {
	return dbm.NewDB("state", dbm.BackendType(cfg.DBBackend), cfg.DBDir())
}
