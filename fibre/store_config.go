package fibre

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	pebbledb "github.com/cockroachdb/pebble/v2"
)

// ObjectStorageConfig configures S3-compatible storage. Credentials use the AWS SDK credential chain.
type ObjectStorageConfig struct {
	Endpoint string `toml:"endpoint"`
	Region   string `toml:"region" comment:"Use auto for Cloudflare R2."`
	Bucket   string `toml:"bucket"`
	Prefix   string `toml:"prefix"`
	// ChainID and ValidatorAddress are derived by the server at startup.
	ChainID          string `toml:"-"`
	ValidatorAddress string `toml:"-"`
}

// Validate removes surrounding whitespace and checks the settings required to access object storage.
func (cfg *ObjectStorageConfig) Validate() error {
	cfg.Region = strings.TrimSpace(cfg.Region)
	cfg.Bucket = strings.TrimSpace(cfg.Bucket)
	cfg.Prefix = strings.TrimSpace(cfg.Prefix)
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("object_storage.endpoint must be an absolute HTTP or HTTPS URL without credentials, query, or fragment")
	}
	if cfg.Region == "" {
		return fmt.Errorf("object_storage.region is required")
	}
	if cfg.Bucket == "" {
		return fmt.Errorf("object_storage.bucket is required")
	}
	if strings.Trim(cfg.Prefix, "/") == "" {
		return fmt.Errorf("object_storage.prefix is required")
	}
	return nil
}

// openObjectStorage opens the backend for object mode or existing object markers.
// Local mode still needs it to read and prune shards written before a mode change.
func (s *Store) openObjectStorage(ctx context.Context, cfg StoreConfig) (shardBackend, error) {
	hasObjects, err := s.hasObjectMarkers(ctx)
	if err != nil {
		return nil, err
	}
	saved, recorded, err := s.readObjectNamespace()
	if err != nil {
		return nil, err
	}
	if hasObjects && !recorded {
		return nil, fmt.Errorf("%w: object markers exist without an object namespace", ErrStoreIntegrity)
	}
	if !hasObjects && cfg.StorageBackend != storageBackendObject {
		return nil, nil
	}
	s.log.Warn("Changing storage_backend only affects new shards. Keep object storage configured and accessible until all object shards are pruned.",
		"storage_backend", cfg.StorageBackend)
	if err := cfg.ObjectStorage.Validate(); err != nil {
		return nil, fmt.Errorf("object storage must remain configured until all object shards are pruned: %w", err)
	}
	if cfg.ObjectStorage.ChainID == "" || cfg.ObjectStorage.ValidatorAddress == "" {
		return nil, fmt.Errorf("chain ID and validator address are required for object storage")
	}
	namespace := namespaceFromConfig(cfg.ObjectStorage)
	mismatch := hasObjects && saved != namespace
	if mismatch {
		s.log.Warn("Object storage namespace changed", "old", saved, "new", namespace,
			"override", cfg.OverrideObjectNamespace)
		if !cfg.OverrideObjectNamespace {
			return nil, fmt.Errorf("%w: object namespace mismatch; restore the previous configuration or migrate objects before using --override-object-namespace", ErrStoreIntegrity)
		}
	}
	s.objectNamespace, err = json.Marshal(namespace)
	if err != nil {
		return nil, fmt.Errorf("encoding object namespace: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.ObjectStorage.Region))
	if err != nil {
		return nil, fmt.Errorf("loading AWS configuration: %w", err)
	}
	// The SDK resolves credentials lazily; fail startup if none can be retrieved.
	if _, err := awsConfig.Credentials.Retrieve(ctx); err != nil {
		return nil, fmt.Errorf("loading object storage credentials: %w", err)
	}
	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(cfg.ObjectStorage.Endpoint)
	})
	if mismatch {
		if err := s.db.Set([]byte(objectNamespaceKey), s.objectNamespace, pebbledb.Sync); err != nil {
			return nil, fmt.Errorf("saving object namespace override: %w", err)
		}
	}
	return newObjectBackend(client, cfg.ObjectStorage.Bucket, cfg.ObjectStorage.Prefix, cfg.ObjectStorage.ChainID, cfg.ObjectStorage.ValidatorAddress), nil
}

// hasObjectMarkers stops at the first valid object marker without reading payloads.
func (s *Store) hasObjectMarkers(ctx context.Context) (bool, error) {
	prefix := []byte(shardKeyPrefix)
	iter, err := s.db.NewIter(&pebbledb.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return false, fmt.Errorf("creating shard iterator: %w", err)
	}
	defer iter.Close()

	for valid := iter.First(); valid; valid = iter.Next() {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		backend, _, err := decodeShardMarkerBackend(iter.Value())
		if err == nil && backend == objectBackendTag {
			return true, nil
		}
	}
	return false, iter.Error()
}
