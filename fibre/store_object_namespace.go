package fibre

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	pebbledb "github.com/cockroachdb/pebble/v2"
)

const objectNamespaceKey = "/meta/object-namespace"

// objectNamespace identifies the location of a validator's object shards.
type objectNamespace struct {
	Endpoint         string `json:"endpoint" toml:"endpoint"`
	Bucket           string `json:"bucket" toml:"bucket"`
	Prefix           string `json:"prefix" toml:"prefix"`
	ChainID          string `json:"chain_id" toml:"-"`
	ValidatorAddress string `json:"validator_address" toml:"-"`
}

func (n objectNamespace) canonical() objectNamespace {
	n.Prefix = path.Clean(strings.Trim(n.Prefix, "/"))
	return n
}

func readObjectNamespace(db *pebbledb.DB) (objectNamespace, bool, error) {
	var namespace objectNamespace
	data, closer, err := db.Get([]byte(objectNamespaceKey))
	if errors.Is(err, pebbledb.ErrNotFound) {
		return namespace, false, nil
	}
	if err != nil {
		return namespace, false, fmt.Errorf("reading object namespace: %w", err)
	}
	defer closer.Close()
	if err := json.Unmarshal(data, &namespace); err != nil {
		return namespace, false, fmt.Errorf("%w: decoding object namespace: %v", ErrStoreIntegrity, err)
	}
	// Reuse configuration validation without storing the region or credentials.
	cfg := ObjectStorageConfig{
		objectNamespace: namespace, Region: "unused",
	}
	if err := cfg.Validate(); err != nil {
		return namespace, false, fmt.Errorf("%w: invalid object namespace: %v", ErrStoreIntegrity, err)
	}
	if namespace.ChainID == "" || namespace.ValidatorAddress == "" || namespace != cfg.canonical() {
		return namespace, false, fmt.Errorf("%w: incomplete or non-canonical object namespace", ErrStoreIntegrity)
	}
	return namespace, true, nil
}

// saveObjectNamespace persists the namespace before uploads can commit shard markers.
func saveObjectNamespace(db *pebbledb.DB, namespace objectNamespace) error {
	data, err := json.Marshal(namespace)
	if err != nil {
		return fmt.Errorf("encoding object namespace: %w", err)
	}
	if err := db.Set([]byte(objectNamespaceKey), data, pebbledb.Sync); err != nil {
		return fmt.Errorf("saving object namespace: %w", err)
	}
	return nil
}
