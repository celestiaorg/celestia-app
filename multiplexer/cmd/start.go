package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/celestiaorg/celestia-app/v6/multiplexer/abci"
	"github.com/celestiaorg/celestia-app/v6/multiplexer/internal"
	dbm "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmtnode "github.com/cometbft/cometbft/node"
	cmtstate "github.com/cometbft/cometbft/state"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/server/types"
	tmnode "github.com/tendermint/tendermint/node"
	tmtypes "github.com/tendermint/tendermint/types"
)

// start initializes and runs the multiplexer with the application.
func start(versions abci.Versions, svrCtx *server.Context, clientCtx client.Context, appCreator types.AppCreator) error {
	// 1. Get and validate server configuration
	svrCfg, err := getAndValidateConfig(svrCtx)
	if err != nil {
		return err
	}

	// 2. Open the state database
	db, err := openDBM(svrCtx.Config)
	if err != nil {
		return err
	}
	// Defer closing the database to ensure it's closed when 'start' exits.
	defer db.Close()

	// 3. Get the current chain state (chain ID and application version)
	chainID, appVersion, err := getState(svrCtx.Config, db)
	if err != nil {
		return fmt.Errorf("failed to get current app state: %w", err)
	}

	svrCtx.Logger.Info("Initializing Celestia application multiplexer", "app_version", appVersion, "chain_id", chainID)

	// 4. Create and start the multiplexer
	multiplexer, err := abci.NewMultiplexer(svrCtx, svrCfg, clientCtx, appCreator, versions, chainID, appVersion)
	if err != nil {
		return err
	}

	defer func() {
		if err := multiplexer.Stop(); err != nil {
			svrCtx.Logger.Error("Failed to stop multiplexer gracefully", "err", err)
		}
	}()

	// Start will either launch the latest app natively, or an embedded app if an older version is specified.
	if err := multiplexer.Start(); err != nil {
		return fmt.Errorf("failed to start multiplexer: %w", err)
	}

	return nil
}

// getState opens the db, fetches the existing state, and determines the current chainID and appVersion.
func getState(cfg *cmtcfg.Config, db dbm.DB) (chainID string, appVersion uint64, err error) {
	genVer, err := internal.GetGenesisVersion(cfg.GenesisFile())
	if err != nil {
		// Fallback to loading state from DB or Genesis Doc Provider if genesis version info is missing.
		if errors.Is(err, internal.ErrGenesisNotFound) {
			return loadStateFromDBOrGenDoc(db, internal.GetGenDocProvider(cfg))
		}

		return "", 0, err
	}

	switch genVer {
	case internal.GenesisVersion1:
		// Load state using the old Tendermint logic for GenesisVersion1
		return loadTmStateFromDBOrGenDoc(db, func() (*tmtypes.GenesisDoc, error) {
			return tmtypes.GenesisDocFromFile(cfg.GenesisFile())
		})

	case internal.GenesisVersion2:
		// Load state using the new CometBFT logic for GenesisVersion2
		return loadStateFromDBOrGenDoc(db, internal.GetGenDocProvider(cfg))
	}

	return "", 0, fmt.Errorf("unknown genesis version: %d", genVer)
}

// loadTmStateFromDBOrGenDoc loads state using the Tendermint (v1) logic.
func loadTmStateFromDBOrGenDoc(db dbm.DB, genDocProvider func() (*tmtypes.GenesisDoc, error)) (string, uint64, error) {
	s, _, err := tmnode.LoadStateFromDBOrGenesisDocProvider(db, genDocProvider)
	if err != nil {
		return "", 0, err
	}
	return s.ChainID, s.Version.Consensus.App, nil
}

// loadStateFromDBOrGenDoc loads state using the CometBFT (v2) logic.
func loadStateFromDBOrGenDoc(db dbm.DB, genDocProvider func() (*cmttypes.GenesisDoc, error)) (string, uint64, error) {
	s, _, err := cmtnode.LoadStateFromDBOrGenesisDocProvider(db, genDocProvider)
	if err != nil {
		return "", 0, err
	}
	// Use cmtstate.State type for consistency (if LoadState returns it, otherwise relies on type inference)
	// Assuming s is of type cmtstate.State or compatible struct that includes ChainID and Version.Consensus.App
	return s.ChainID, s.Version.Consensus.App, nil
}

// getAndValidateConfig retrieves and validates the server configuration.
func getAndValidateConfig(svrCtx *server.Context) (serverconfig.Config, error) {
	config, err := serverconfig.GetConfig(svrCtx.Viper)
	if err != nil {
		return config, err
	}

	if err := config.ValidateBasic(); err != nil {
		return config, err
	}

	// Check that the gRPC listen address is explicitly set.
	if strings.TrimSpace(svrCtx.Config.RPC.GRPCListenAddress) == "" {
		return config, errors.New("must set the RPC GRPC listen address (grpc_laddr) in config.toml or by flag")
	}

	return config, nil
}

// openDBM creates a new state database connection.
func openDBM(cfg *cmtcfg.Config) (dbm.DB, error) {
	return dbm.NewDB("state", dbm.BackendType(cfg.DBBackend), cfg.DBDir())
}
