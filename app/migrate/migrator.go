package migrate

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"cosmossdk.io/log"
	cosmosdb "github.com/cosmos/cosmos-db"
	"golang.org/x/time/rate"
)

// progressInterval is how often progress is logged during migration.
const progressInterval = 2 * time.Minute

// MigrationStatus reports the current state of the background migration.
type MigrationStatus struct {
	Databases map[string]DBStatus
	Done      bool
}

// DBStatus reports the migration state of a single database.
type DBStatus struct {
	KeysMigrated  int64
	BytesMigrated int64
	Done          bool
}

// Migrator performs background database migration from LevelDB to PebbleDB.
type Migrator struct {
	sourceDBs map[string]cosmosdb.DB
	destDir   string
	rateMBs   int
	logger    log.Logger

	mu     sync.Mutex
	status MigrationStatus
}

// NewMigrator creates a new background migrator.
// sourceDBs maps database names (e.g. "application", "blockstore") to live DB handles.
// destDir is the directory where PebbleDB databases will be created (e.g. data_pebble/).
// rateMBs is the global rate limit in MB/s for writes.
func NewMigrator(sourceDBs map[string]cosmosdb.DB, destDir string, rateMBs int, logger log.Logger) *Migrator {
	return &Migrator{
		sourceDBs: sourceDBs,
		destDir:   destDir,
		rateMBs:   rateMBs,
		logger:    logger,
		status: MigrationStatus{
			Databases: make(map[string]DBStatus),
		},
	}
}

// Start launches background goroutines to copy all source databases to PebbleDB.
// It blocks until all databases are copied or the context is cancelled.
func (m *Migrator) Start(ctx context.Context) error {
	if err := os.MkdirAll(m.destDir, 0o755); err != nil {
		return fmt.Errorf("failed to create dest directory %s: %w", m.destDir, err)
	}

	var limiter *rate.Limiter
	if m.rateMBs > 0 {
		bytesPerSec := rate.Limit(m.rateMBs) * 1024 * 1024
		limiter = rate.NewLimiter(bytesPerSec, m.rateMBs*1024*1024)
	}

	m.logger.Info("Background migration started", "dest", m.destDir, "rate_mb_s", m.rateMBs, "databases", len(m.sourceDBs))

	var wg sync.WaitGroup
	errCh := make(chan error, len(m.sourceDBs))

	for name, srcDB := range m.sourceDBs {
		wg.Add(1)
		go func(name string, srcDB cosmosdb.DB) {
			defer wg.Done()
			if err := m.migrateOneDB(ctx, name, srcDB, limiter); err != nil {
				if ctx.Err() != nil {
					m.logger.Info("Background migration stopped", "db", name, "reason", "context cancelled")
					return
				}
				m.logger.Error("Background migration failed", "db", name, "err", err)
				errCh <- fmt.Errorf("[%s] %w", name, err)
			}
		}(name, srcDB)
	}

	wg.Wait()
	close(errCh)

	var firstErr error
	for err := range errCh {
		if firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		return firstErr
	}

	m.mu.Lock()
	m.status.Done = true
	m.mu.Unlock()

	m.logger.Info("Background migration complete. Stop your node and run: migrate-db --auto-swap")
	return nil
}

// Status returns a snapshot of the current migration progress.
func (m *Migrator) Status() MigrationStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := MigrationStatus{
		Databases: make(map[string]DBStatus, len(m.status.Databases)),
		Done:      m.status.Done,
	}
	for k, v := range m.status.Databases {
		s.Databases[k] = v
	}
	return s
}

// migrateOneDB copies a single source database to PebbleDB using shared CopyDB logic.
func (m *Migrator) migrateOneDB(ctx context.Context, dbName string, srcDB cosmosdb.DB, limiter *rate.Limiter) error {
	destDB, err := cosmosdb.NewDB(dbName, cosmosdb.PebbleDBBackend, m.destDir)
	if err != nil {
		return fmt.Errorf("failed to open dest PebbleDB: %w", err)
	}
	defer destDB.Close()

	startTime := time.Now()
	lastLogTime := time.Now()

	result, err := CopyDB(ctx, srcDB, destDB, CopyDBOptions{
		Limiter: limiter,
		ProgressFn: func(keys, bytesTotal int64) {
			m.mu.Lock()
			m.status.Databases[dbName] = DBStatus{
				KeysMigrated:  keys,
				BytesMigrated: bytesTotal,
			}
			m.mu.Unlock()

			if time.Since(lastLogTime) >= progressInterval {
				elapsed := time.Since(startTime)
				rateMBs := float64(bytesTotal) / elapsed.Seconds() / 1024 / 1024
				m.logger.Info("Background migration progress",
					"db", dbName,
					"keys", keys,
					"bytes", HumanBytes(bytesTotal),
					"rate_mb_s", fmt.Sprintf("%.1f", rateMBs),
					"elapsed", elapsed.Round(time.Second).String(),
				)
				lastLogTime = time.Now()
			}
		},
	})
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.status.Databases[dbName] = DBStatus{
		KeysMigrated:  result.KeysCopied,
		BytesMigrated: result.BytesCopied,
		Done:          true,
	}
	m.mu.Unlock()

	m.logger.Info("Background migration complete for database",
		"db", dbName,
		"keys", result.KeysCopied,
		"bytes", HumanBytes(result.BytesCopied),
		"elapsed", time.Since(startTime).Round(time.Second).String(),
	)
	return nil
}
