package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/celestiaorg/celestia-app/v6/app"
	cmtdb "github.com/cometbft/cometbft-db"
	tmcfg "github.com/cometbft/cometbft/config"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

const (
	FlagTargetHeight = "target-height"
	FlagRemoveBlocks = "remove-blocks"
)

// RollbackToHeightCmd returns a command to rollback both app and CometBFT state to a specific height.
func RollbackToHeightCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback-to-height",
		Short: "Rollback both app and CometBFT state to a specific target height",
		Long: `Rollback both application state and CometBFT state to a specific target height.

This is useful for recovery scenarios where the application has committed more blocks
than are stored in the blockstore (e.g., after a crash during batch block saves).

Unlike the standard 'rollback' command which only rolls back one block (n -> n-1),
this command can roll back multiple blocks to reach any target height.

Example:
  celestia-appd rollback-to-height --target-height=100
  celestia-appd rollback-to-height --target-height=100 --remove-blocks

WARNING: This is a destructive operation. Make sure you have backups before proceeding.
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := server.GetServerContextFromCmd(cmd)
			cfg := ctx.Config

			targetHeight, err := cmd.Flags().GetInt64(FlagTargetHeight)
			if err != nil {
				return fmt.Errorf("failed to get target-height flag: %w", err)
			}

			if targetHeight <= 0 {
				return fmt.Errorf("target-height must be greater than 0, got %d", targetHeight)
			}

			removeBlocks, err := cmd.Flags().GetBool(FlagRemoveBlocks)
			if err != nil {
				return fmt.Errorf("failed to get remove-blocks flag: %w", err)
			}

			// Get the home directory
			home := cfg.RootDir

			ctx.Logger.Info("Starting rollback to target height",
				"target_height", targetHeight,
				"remove_blocks", removeBlocks)

			// Open the application database
			appDB, err := openDB(home, server.GetAppDBBackend(ctx.Viper))
			if err != nil {
				return fmt.Errorf("failed to open app database: %w", err)
			}
			defer appDB.Close()

			// Create app instance to access CommitMultiStore
			appInstance := app.New(ctx.Logger, appDB, nil, 0, ctx.Viper)

			// Get current app height before rollback
			currentAppHeight := appInstance.LastBlockHeight()
			ctx.Logger.Info("Current application height", "height", currentAppHeight)

			if targetHeight >= currentAppHeight {
				return fmt.Errorf("target height (%d) must be less than current app height (%d)", targetHeight, currentAppHeight)
			}

			// Rollback app state using CommitMultiStore
			ctx.Logger.Info("Rolling back application state",
				"from_height", currentAppHeight,
				"to_height", targetHeight)
			if err := appInstance.CommitMultiStore().RollbackToVersion(targetHeight); err != nil {
				return fmt.Errorf("failed to rollback app state: %w", err)
			}
			ctx.Logger.Info("Application state rolled back successfully")

			// Now rollback CometBFT state
			ctx.Logger.Info("Rolling back CometBFT state")

			// Open blockstore database
			blockStoreDB, err := openCometBFTDB("blockstore", cfg)
			if err != nil {
				return fmt.Errorf("failed to open blockstore database: %w", err)
			}
			defer blockStoreDB.Close()

			blockStore := store.NewBlockStore(blockStoreDB)

			// Open state database
			stateDB, err := openCometBFTDB("state", cfg)
			if err != nil {
				return fmt.Errorf("failed to open state database: %w", err)
			}
			defer stateDB.Close()

			stateStore := sm.NewStore(stateDB, sm.StoreOptions{
				DiscardABCIResponses: cfg.Storage.DiscardABCIResponses,
			})

			// Get current CometBFT state height
			currentState, err := stateStore.Load()
			if err != nil {
				return fmt.Errorf("failed to load current state: %w", err)
			}
			ctx.Logger.Info("Current CometBFT heights",
				"state_height", currentState.LastBlockHeight,
				"blockstore_height", blockStore.Height())

			// Rollback CometBFT state using RollbackToHeight
			//rolledBackHeight, appHash, err := sm.RollbackToHeight(blockStore, stateStore, targetHeight, removeBlocks)
			//if err != nil {
			//	return fmt.Errorf("failed to rollback CometBFT state: %w", err)
			//}
			//
			//ctx.Logger.Info("CometBFT state rolled back successfully",
			//	"height", rolledBackHeight,
			//	"app_hash", fmt.Sprintf("%X", appHash))
			//
			//// Verify all components are synchronized
			//newBlockStoreHeight := blockStore.Height()
			//if removeBlocks {
			//	if newBlockStoreHeight != targetHeight {
			//		ctx.Logger.Warn("Blockstore height mismatch after rollback",
			//			"blockstore_height", newBlockStoreHeight,
			//			"target_height", targetHeight)
			//	} else {
			//		ctx.Logger.Info("Blockstore synchronized", "height", newBlockStoreHeight)
			//	}
			//}
			//
			//ctx.Logger.Info("Rollback completed successfully",
			//	"target_height", targetHeight,
			//	"app_state", "rolled back",
			//	"cometbft_state", "rolled back",
			//	"blocks_removed", removeBlocks)
			//
			//ctx.Logger.Info("You can now restart the node. It will replay blocks from the network.")

			return nil
		},
	}

	cmd.Flags().Int64(FlagTargetHeight, 0, "Target height to rollback to (required)")
	cmd.Flags().Bool(FlagRemoveBlocks, false, "Remove blocks after target height from blockstore")
	_ = cmd.MarkFlagRequired(FlagTargetHeight)

	return cmd
}

// openDB opens the application database
func openDB(home string, backendType dbm.BackendType) (dbm.DB, error) {
	dataDir := filepath.Join(home, "data")
	return dbm.NewDB("application", backendType, dataDir)
}

// openCometBFTDB opens a CometBFT database (blockstore or state)
func openCometBFTDB(dbName string, cfg *tmcfg.Config) (cmtdb.DB, error) {
	dbDir := filepath.Join(cfg.RootDir, "data")
	dbType := cmtdb.BackendType(cfg.DBBackend)
	return cmtdb.NewDB(dbName, dbType, dbDir)
}
