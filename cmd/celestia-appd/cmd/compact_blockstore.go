package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cosmossdk.io/log"
	cometbftdb "github.com/cometbft/cometbft-db"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

const flagYes = "yes"

// compactBlockstoreDBs lists the CometBFT-owned databases this command compacts,
// in the order they will be processed.
var compactBlockstoreDBs = []string{"blockstore", "state"}

func compactBlockstoreCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compact-blockstore",
		Short: "Compact the CometBFT blockstore and state databases",
		Long: `Compact the CometBFT blockstore and state databases.

This command runs an offline compaction of blockstore.db and state.db under
the configured db_dir (defaults to <home>/data).

Compaction reclaims disk space and improves read performance after large
numbers of deletes (for example, after block pruning). It does not modify or
lose any committed data; it only reorganizes existing on-disk storage.

The node MUST be stopped before running this command. While the node is
running, the database files are locked and the command will fail with a lock
error. Depending on database size and storage performance, compaction can
take from a few seconds to many minutes.

The database backend is read from <home>/config/config.toml (db_backend).
Supported backends: goleveldb, pebbledb. Any other backend will be rejected
with an error.

The application database (application.db) is NOT compacted by this command.

Examples:
  celestia-appd compact-blockstore --home /var/lib/celestia-app
  celestia-appd compact-blockstore --home /var/lib/celestia-app -y
`,
		RunE: runCompactBlockstore,
	}
	cmd.Flags().BoolP(flagYes, "y", false, "Skip the interactive confirmation prompt")
	return cmd
}

func runCompactBlockstore(cmd *cobra.Command, _ []string) error {
	sctx := server.GetServerContextFromCmd(cmd)
	logger := sctx.Logger

	backendStr := strings.ToLower(strings.TrimSpace(sctx.Config.DBBackend))
	switch backendStr {
	case "goleveldb", "pebbledb":
	default:
		return fmt.Errorf("unsupported db_backend %q: only goleveldb and pebbledb are supported", backendStr)
	}
	backend := cometbftdb.BackendType(backendStr)
	dataDir := sctx.Config.DBDir()

	for _, name := range compactBlockstoreDBs {
		path := filepath.Join(dataDir, name+".db")
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("expected database not found at %s: %w", path, err)
		}
	}

	yes, err := cmd.Flags().GetBool(flagYes)
	if err != nil {
		return err
	}
	if !yes {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(),
			"About to compact %v under %s (backend=%s).\nThe node must be stopped. Continue? [y/N]: ",
			compactBlockstoreDBs, dataDir, backendStr); err != nil {
			return err
		}
		reader := bufio.NewReader(cmd.InOrStdin())
		line, err := reader.ReadString('\n')
		// io.EOF without input is treated as "no answer" and falls through to the
		// non-"y" check below; any other read error is fatal.
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		line = strings.TrimSpace(strings.ToLower(line))
		if line != "y" && line != "yes" {
			return fmt.Errorf("aborted by user")
		}
	}

	for _, name := range compactBlockstoreDBs {
		if err := compactOneCometBFTDB(logger, name, backend, dataDir); err != nil {
			return fmt.Errorf("compact %s: %w", name, err)
		}
	}
	return nil
}

func compactOneCometBFTDB(logger log.Logger, name string, backend cometbftdb.BackendType, dataDir string) error {
	dbPath := filepath.Join(dataDir, name+".db")

	db, err := cometbftdb.NewDB(name, backend, dataDir)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			logger.Warn("error closing database", "db", name, "err", cerr)
		}
	}()

	logger.Info("compacting database",
		"db", name,
		"backend", string(backend),
		"path", dbPath,
	)
	start := time.Now()

	if err := db.Compact(nil, nil); err != nil {
		return fmt.Errorf("compact: %w", err)
	}

	logger.Info("database compaction complete",
		"db", name,
		"elapsed", time.Since(start).String(),
	)
	return nil
}
