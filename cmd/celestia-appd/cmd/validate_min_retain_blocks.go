package cmd

import (
	"fmt"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// validateMinRetainBlocks ensures the min-retain-blocks configuration meets
// the minimum required value for state sync. This is NOT bypassable because
// it's critical for network health that nodes retain enough blocks for other
// nodes to sync from state sync snapshots.
//
// Unlike other hooks that modify sctx.Config (CometBFT config), this hook
// reads sctx.Viper because min-retain-blocks is an app-level config
// (app.toml) that the Cosmos SDK reads from viper via appOpts.Get() when
// creating the app in server/util.go.
func validateMinRetainBlocks(cmd *cobra.Command, _ log.Logger) error {
	sctx := server.GetServerContextFromCmd(cmd)
	v := sctx.Viper

	// Read current values from viper (app.toml config)
	minRetainBlocks := v.GetUint64(server.FlagMinRetainBlocks)

	// 0 means "prune nothing" - don't override as the user wants to keep all blocks
	if minRetainBlocks == 0 {
		return nil
	}

	// Check if flag was explicitly set via CLI
	flag := cmd.Flags().Lookup(server.FlagMinRetainBlocks)
	if flag != nil && flag.Changed {
		// Flag was explicitly passed on command line
		// Error if value is 1 to requiredMinRetain-1 (0 is allowed as "prune nothing")
		if minRetainBlocks > 0 && minRetainBlocks < appconsts.MinRetainBlocks {
			return fmt.Errorf("--%s value %d is below minimum %d (use 0 to disable pruning)",
				server.FlagMinRetainBlocks, minRetainBlocks, appconsts.MinRetainBlocks)
		}
		// CLI value is valid (either 0 or >= requiredMinRetain), use as-is
		return nil
	}

	// Value came from config file - error if too low
	if minRetainBlocks < appconsts.MinRetainBlocks {
		return fmt.Errorf("min-retain-blocks value %d in app.toml is below minimum %d, please update min-retain-blocks to 0 (retain all blocks) or >= %d",
			minRetainBlocks, appconsts.MinRetainBlocks, appconsts.MinRetainBlocks)
	}

	return nil
}
