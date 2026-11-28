package cmd

import (
	"path/filepath"
	"strings"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v12/app"
	"github.com/celestiaorg/celestia-app/v12/app/appdb"
	dbm "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/libs/walfsync"
	"github.com/cometbft/cometbft/node"
	"github.com/cosmos/cosmos-sdk/server"
	servercmtlog "github.com/cosmos/cosmos-sdk/server/log"
	"github.com/spf13/cobra"
)

// addDBWALFsyncFlag adds the one-shot --accept_unsynced_db_crash flag. The
// multiplexer forwards it to the embedded apps that support it.
func addDBWALFsyncFlag(startCmd *cobra.Command) {
	startCmd.Flags().Bool(walfsync.AcceptFlag, false,
		"start even if the OS crashed while database WAL fsyncs were skipped (pass it for one start only)")
}

// setupDBWALFsync runs before the start command creates the app and the node.
// It handles --accept_unsynced_db_crash, checks the embedded app's crash
// marker, checks the node's marker over the app data directory when that is
// outside the node's db_dir, and makes the app DB of this process follow
// [storage] db_wal_fsync.
func setupDBWALFsync(cmd *cobra.Command, logger log.Logger) error {
	sctx := server.GetServerContextFromCmd(cmd)
	cfg := sctx.Config
	accept, err := cmd.Flags().GetBool(walfsync.AcceptFlag)
	if err != nil {
		return err
	}
	// An embedded app killed before its Close leaves its own marker.
	found, err := appdb.CheckEmbeddedAppMarker(cfg.RootDir, accept)
	if err != nil {
		return err
	}
	if found {
		logger.Info("An embedded app stopped while app DB WAL fsyncs were skipped; the app DB WAL files were fsynced")
	}
	appData := filepath.Join(cfg.RootDir, "data")
	if !isWithin(appData, cfg.DBDir()) {
		// The node checks only its own directories when it starts.
		if _, err := walfsync.CheckMarker(dbWALDirs(cfg, appData), accept); err != nil {
			return err
		}
	}
	if accept {
		if err := node.AcceptUnsyncedDBCrash(cfg, servercmtlog.CometLoggerWrapper{Logger: logger}); err != nil {
			return err
		}
	}
	if dbm.BackendType(cfg.DBBackend) == dbm.PebbleDBBackend && cfg.Storage.DBWALFsync != walfsync.ModeAlways &&
		server.GetAppDBBackend(sctx.Viper) == "pebbledb" {
		appdb.WrapWALFsync()
	}
	return nil
}

// clearDBWALFsyncMarkers removes the crash markers after a rollback, which is
// one of the recovery paths the marker asks the operator to choose.
func clearDBWALFsyncMarkers(cfg *cmtcfg.Config) error {
	appData := filepath.Join(cfg.RootDir, "data")
	if _, err := walfsync.CheckMarker(dbWALDirs(cfg, appData), true); err != nil {
		return err
	}
	_, err := appdb.CheckEmbeddedAppMarker(cfg.RootDir, true)
	return err
}

// addRollbackMarkerClearing clears the crash markers after a successful
// rollback, native or passthrough.
func addRollbackMarkerClearing(rootCmd *cobra.Command) error {
	rollbackCmd, _, err := rootCmd.Find([]string{"rollback"})
	if err != nil {
		return err
	}
	rollbackCmd.PostRunE = func(cmd *cobra.Command, _ []string) error {
		return clearDBWALFsyncMarkers(server.GetServerContextFromCmd(cmd).Config)
	}
	for _, c := range rootCmd.Commands() {
		if c.Name() == "passthrough" {
			c.PostRunE = func(_ *cobra.Command, args []string) error {
				if len(args) < 2 || !strings.EqualFold(args[1], "rollback") {
					return nil
				}
				cfg, err := cometConfig(app.NodeHome)
				if err != nil {
					return err
				}
				return clearDBWALFsyncMarkers(cfg)
			}
		}
	}
	return nil
}

// dbWALDirs lists the directories with node or app databases, the node's
// db_dir (which holds the marker) first.
func dbWALDirs(cfg *cmtcfg.Config, appData string) []string {
	dirs := []string{cfg.DBDir()}
	for _, d := range []string{cfg.BlockstoreDir(), appData} {
		if !isWithin(d, dirs[0]) {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// isWithin reports whether path is dir or below it.
func isWithin(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
