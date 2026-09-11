package fibre

import (
	"fmt"
	"log/slog"
)

// StoreConfig contains configuration options for the [Store].
type StoreConfig struct {
	// StorageBackend selects the backend for new shards: local or object.
	StorageBackend string `toml:"storage_backend"`
	// ObjectStorage must remain configured until all object shards are pruned.
	ObjectStorage ObjectStorageConfig `toml:"object_storage"`
	// Path is the path to the store directory.
	Path string `toml:"-"`
	// Log defaults to [slog.Default] when nil.
	Log *slog.Logger `toml:"-"`
}

// DefaultStoreConfig returns a [StoreConfig] with default values.
func DefaultStoreConfig() StoreConfig {
	return StoreConfig{StorageBackend: storageBackendLocal}
}

// Validate checks that the StoreConfig is valid and fills in defaults for
// unset fields.
func (cfg *StoreConfig) Validate() error {
	if cfg.StorageBackend == "" {
		cfg.StorageBackend = storageBackendLocal
	}
	switch cfg.StorageBackend {
	case storageBackendLocal:
	case storageBackendObject:
		if err := cfg.ObjectStorage.Validate(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("storage_backend must be local or object")
	}
	if cfg.Path == "" {
		return fmt.Errorf("store path is required")
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return nil
}
