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

type objectNamespace struct {
	Endpoint         string `json:"endpoint"`
	Bucket           string `json:"bucket"`
	Prefix           string `json:"prefix"`
	ChainID          string `json:"chain_id"`
	ValidatorAddress string `json:"validator_address"`
}

func namespaceFromConfig(cfg ObjectStorageConfig) objectNamespace {
	return objectNamespace{
		Endpoint: cfg.Endpoint, Bucket: cfg.Bucket,
		Prefix:  path.Clean(strings.Trim(cfg.Prefix, "/")),
		ChainID: cfg.ChainID, ValidatorAddress: cfg.ValidatorAddress,
	}
}

func (s *Store) readObjectNamespace() (objectNamespace, bool, error) {
	var namespace objectNamespace
	data, closer, err := s.db.Get([]byte(objectNamespaceKey))
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
		Endpoint: namespace.Endpoint, Region: "unused", Bucket: namespace.Bucket, Prefix: namespace.Prefix,
		ChainID: namespace.ChainID, ValidatorAddress: namespace.ValidatorAddress,
	}
	if err := cfg.Validate(); err != nil {
		return namespace, false, fmt.Errorf("%w: invalid object namespace: %v", ErrStoreIntegrity, err)
	}
	if namespace.ChainID == "" || namespace.ValidatorAddress == "" || namespace != namespaceFromConfig(cfg) {
		return namespace, false, fmt.Errorf("%w: incomplete or non-canonical object namespace", ErrStoreIntegrity)
	}
	return namespace, true, nil
}
