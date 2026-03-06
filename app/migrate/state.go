package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AllDatabases is the canonical list of databases to migrate.
var AllDatabases = []string{
	"application",
	"blockstore",
	"state",
	"tx_index",
	"evidence",
}

// MigrationState tracks overall migration progress across restarts.
type MigrationState struct {
	StartedAt time.Time          `json:"started_at"`
	NoBackup  bool               `json:"no_backup"`
	Databases map[string]DBState `json:"databases"`
}

// DBState tracks the migration status of a single database.
type DBState struct {
	Status        string    `json:"status"` // "pending", "in_progress", "migrated", "source_deleted"
	KeysMigrated  int64     `json:"keys_migrated"`
	BytesMigrated int64     `json:"bytes_migrated"`
	CompletedAt   time.Time `json:"completed_at,omitempty"`
}

// LoadState reads migration state from the dest directory. Returns nil if no state file exists.
func LoadState(destDir string) (*MigrationState, error) {
	path := filepath.Join(destDir, ".migration_state.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state MigrationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("corrupt migration state file: %w", err)
	}
	return &state, nil
}

// SaveState atomically writes migration state to the dest directory.
func SaveState(state *MigrationState, destDir string) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := filepath.Join(destDir, ".migration_state.json.tmp")
	finalPath := filepath.Join(destDir, ".migration_state.json")
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, finalPath)
}

// HumanBytes formats a byte count as a human-readable string.
func HumanBytes(b int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
		tb = 1024 * gb
	)
	switch {
	case b >= tb:
		return fmt.Sprintf("%.2f TB", float64(b)/float64(tb))
	case b >= gb:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
