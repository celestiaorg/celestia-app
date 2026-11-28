package cmd

import (
	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/internal/walfsync"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/tendermint/tendermint/libs/log"
)

// addWALFsyncFlag adds the one-shot --accept_unsynced_db_crash flag, which the
// multiplexer forwards from its own start command.
func addWALFsyncFlag(startCmd *cobra.Command) {
	startCmd.Flags().Bool(walfsync.AcceptFlag, false,
		"start even if the OS crashed while app DB WAL fsyncs were skipped (pass it for one start only)")
}

// setupWALFsync checks the app DB crash marker and applies [storage]
// db_wal_fsync to the app DB before the start command opens it.
func setupWALFsync(cmd *cobra.Command, logger log.Logger) error {
	sctx := server.GetServerContextFromCmd(cmd)
	withComet, err := cmd.Flags().GetBool(flagWithTendermint)
	if err != nil {
		return err
	}
	accept, err := cmd.Flags().GetBool(walfsync.AcceptFlag)
	if err != nil {
		return err
	}
	return app.PrepareWALFsync(sctx.Viper, sctx.Config.RootDir, withComet, accept, logger)
}
