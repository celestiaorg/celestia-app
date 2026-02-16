package cmd

import (
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// overrideMinRetainBlocks overrides the min-retain-blocks configuration to meet
// the minimum required value for state sync. This ensures nodes retain enough
// blocks for other nodes to sync from state sync snapshots.
func overrideMinRetainBlocks(cmd *cobra.Command, logger log.Logger) error {
	sctx := server.GetServerContextFromCmd(cmd)
	minRetainBlocks := sctx.Viper.GetUint64(server.FlagMinRetainBlocks)

	if minRetainBlocks == 0 || minRetainBlocks >= appconsts.MinRetainBlocks {
		return nil
	}

	logger.Info("Overriding min-retain-blocks to minimum",
		"configured", minRetainBlocks,
		"minimum", appconsts.MinRetainBlocks,
	)
	sctx.Viper.Set(server.FlagMinRetainBlocks, appconsts.MinRetainBlocks)
	return nil
}
