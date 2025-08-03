package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/celestiaorg/celestia-app/v6/multiplexer/abci"
	"github.com/celestiaorg/celestia-app/v6/multiplexer/internal"
	dbm "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/node"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/server/types"
	tmnode "github.com/tendermint/tendermint/node"
	tmtypes "github.com/tendermint/tendermint/types"
)

func start(versions abci.Versions, svrCtx *server.Context, clientCtx client.Context, appCreator types.AppCreator) error {
	svrCfg, err := getAndValidateConfig(svrCtx)
	if err != nil {
		return err
	}

	chainID, appVersion, err := getState(svrCtx.Config)
	if err != nil {
		return fmt.Errorf("failed to get current app state: %w", err)
	}

	svrCtx.Logger.Info("initializing multiplexer", "app_version", appVersion, "chain_id", chainID)

	multiplexer, err := abci.NewMultiplexer(svrCtx, svrCfg, clientCtx, appCreator, versions, chainID, appVersion)
	if err != nil {
		return err
	}

	defer func() {
		if err := multiplexer.Stop(); err != nil {
			svrCtx.Logger.Error("failed to stop multiplexer", "err", err)
		}
	}()

	// Start will either start the latest app natively, or an embedded app if one is specified.
	if err := multiplexer.Start(); err != nil {
		return fmt.Errorf("failed to start multiplexer: %w", err)
	}

	return nil
}

// getState opens the db and fetches the existing state.
func getState(cfg *cmtcfg.Config) (chainId string, appVersion uint64, err error) {
	db, err := openDBM(cfg)
	if err != nil {
		return "", 0, err
	}
	defer db.Close()

	genVer, err := internal.GetGenesisVersion(cfg.GenesisFile())
	if err != nil {
		// fallback to latest version if the genesis version doesn't exist
		if errors.Is(err, internal.ErrGenesisNotFound) {
			s, _, err := node.LoadStateFromDBOrGenesisDocProvider(db, internal.GetGenDocProvider(cfg))
			return s.ChainID, s.Version.Consensus.App, err
		}

		return "", 0, err
	}

	switch genVer {
	case internal.GenesisVersion1:
		s, _, err := tmnode.LoadStateFromDBOrGenesisDocProvider(
			db,
			func() (*tmtypes.GenesisDoc, error) {
				return tmtypes.GenesisDocFromFile(cfg.GenesisFile())
			},
		)
		if err != nil {
			return "", 0, err
		}

		appVersion = s.Version.Consensus.App
		chainId = s.ChainID

	case internal.GenesisVersion2:
		s, _, err := node.LoadStateFromDBOrGenesisDocProvider(db, internal.GetGenDocProvider(cfg))
		if err != nil {
			return "", 0, err
		}

		appVersion = s.Version.Consensus.App
		chainId = s.ChainID
	}

	return chainId, appVersion, nil
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
