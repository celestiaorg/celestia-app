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
func validateMinRetainBlocks(cmd *cobra.Command, _ log.Logger) error {
	sctx := server.GetServerContextFromCmd(cmd)
	minRetainBlocks := sctx.Viper.GetUint64(server.FlagMinRetainBlocks)

	if minRetainBlocks == 0 || minRetainBlocks >= appconsts.MinRetainBlocks {
		return nil
	}

	return fmt.Errorf("min-retain-blocks %d is below minimum %d, please set to 0 (retain all blocks) or >= %d", minRetainBlocks, appconsts.MinRetainBlocks, appconsts.MinRetainBlocks)
}
