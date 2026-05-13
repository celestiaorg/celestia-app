package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cosmossdk.io/log"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

var dbsToCompact = []string{"application", "blockstore", "state"}

func compactDBs(cmd *cobra.Command, logger log.Logger) error {
	sctx := server.GetServerContextFromCmd(cmd)
	backend := server.GetAppDBBackend(sctx.Viper)
	dataDir := filepath.Join(sctx.Config.RootDir, "data")

	for _, name := range dbsToCompact {
		if err := compactOne(logger, name, backend, dataDir); err != nil {
			logger.Error("database compaction failed", "db", name, "err", err)
		}
	}
	return nil
}

func compactOne(logger log.Logger, name string, backend dbm.BackendType, dataDir string) error {
	if _, err := os.Stat(filepath.Join(dataDir, name+".db")); err != nil {
		logger.Info("skipping compaction, database not found", "db", name)
		return nil
	}

	db, err := dbm.NewDB(name, backend, dataDir)
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	defer db.Close()

	logger.Info("compacting database", "db", name, "backend", backend)
	start := time.Now()

	switch v := db.(type) {
	case *dbm.PebbleDB:
		if err := v.DB().Compact(nil, []byte{0xff, 0xff, 0xff, 0xff}, true); err != nil {
			return fmt.Errorf("pebble compact %s: %w", name, err)
		}
	case *dbm.GoLevelDB:
		if err := v.ForceCompact(nil, nil); err != nil {
			return fmt.Errorf("goleveldb compact %s: %w", name, err)
		}
	default:
		logger.Info("backend does not support compaction, skipping", "db", name, "type", fmt.Sprintf("%T", db))
		return nil
	}

	logger.Info("database compaction complete", "db", name, "elapsed", time.Since(start).String())
	return nil
}
