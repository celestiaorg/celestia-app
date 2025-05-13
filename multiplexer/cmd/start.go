package cmd

import (
	"errors"
	"fmt"
	"strings"

	dbm "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/node"
	cmtstate "github.com/cometbft/cometbft/proto/tendermint/state"
	cmtversion "github.com/cometbft/cometbft/proto/tendermint/version"
	"github.com/cometbft/cometbft/state"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/server/types"
	tmnode "github.com/tendermint/tendermint/node"
	tmstate "github.com/tendermint/tendermint/state"
	tmtypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/v4/multiplexer/abci"
	"github.com/celestiaorg/celestia-app/v4/multiplexer/internal"
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

	genVer, err := internal.GetGenesisVersion(cfg.GenesisFile())
	if err != nil {
		// fallback to latest version if the genesis version doesn't exist
		if errors.Is(err, internal.ErrGenesisNotFound) {
			s, _, err := node.LoadStateFromDBOrGenesisDocProvider(db, internal.GetGenDocProvider(cfg))
			return s, err
		}

		return state.State{}, err
	}

	var s state.State

	switch genVer {
	case internal.GenesisVersion1:
		var s1 tmstate.State
		s1, _, err = tmnode.LoadStateFromDBOrGenesisDocProvider(
			db,
			func() (*tmtypes.GenesisDoc, error) {
				return tmtypes.GenesisDocFromFile(cfg.GenesisFile())
			},
		)
		if err != nil {
			return state.State{}, err
		}

		// we only fill the app version and the chain id
		// the rest of the state is not needed by the multiplexer
		s = state.State{
			ChainID: s1.ChainID,
			Version: cmtstate.Version{
				Consensus: cmtversion.Consensus{
					App: s1.Version.Consensus.App,
				},
			},
		}
	case internal.GenesisVersion2:
		s, _, err = node.LoadStateFromDBOrGenesisDocProvider(db, internal.GetGenDocProvider(cfg))
		if err != nil {
			return state.State{}, err
		}
	}

	fmt.Printf("state: %v\n", s)
	fmt.Printf("state.Version.Consensus.App: %v\n", s.Version.Consensus.App)
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

	if strings.TrimSpace(svrCtx.Config.RPC.GRPCListenAddress) == "" {
		return config, fmt.Errorf("must set the RPC GRPC listen address in config.toml (grpc_laddr) or by flag (--rpc.grpc_laddr)")
	}

	return config, nil
}

func openDBM(cfg *cmtcfg.Config) (dbm.DB, error) {
	return dbm.NewDB("state", dbm.BackendType(cfg.DBBackend), cfg.DBDir())
}
