package cmd

import (
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// overrideIAVLCacheSize raises iavl-cache-size to the default when the
// configured value is lower.
func overrideIAVLCacheSize(cmd *cobra.Command, logger log.Logger) error {
	bypass, err := cmd.Flags().GetBool(bypassOverridesFlagKey)
	if err == nil && bypass {
		return nil
	}

	sctx := server.GetServerContextFromCmd(cmd)
	iavlCacheSize := sctx.Viper.GetUint64(server.FlagIAVLCacheSize)
	minIAVLCacheSize := app.DefaultAppConfig().IAVLCacheSize

	if iavlCacheSize >= minIAVLCacheSize {
		return nil
	}

	logger.Info("Overriding iavl-cache-size to minimum",
		"configured", iavlCacheSize,
		"minimum", minIAVLCacheSize,
	)
	sctx.Viper.Set(server.FlagIAVLCacheSize, minIAVLCacheSize)
	return nil
}
