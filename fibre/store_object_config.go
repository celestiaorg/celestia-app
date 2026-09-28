package fibre

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	pebbledb "github.com/cockroachdb/pebble/v2"
)

const defaultObjectRequestTimeout = 30 * time.Second

// ObjectStorageConfig configures S3-compatible storage. Credentials use the AWS SDK credential chain.
type ObjectStorageConfig struct {
	// ChainID and ValidatorAddress are derived by the server at startup.
	objectNamespace
	Region string `toml:"region" comment:"Use auto for Cloudflare R2."`
	// RequestTimeout bounds an object operation, including retries and response reads.
	RequestTimeout time.Duration `toml:"request_timeout" comment:"Timeout per object operation in nanoseconds. Zero uses 30 seconds."`
	// OverrideNamespace accepts a namespace change after operator migration.
	OverrideNamespace bool `toml:"-"`
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
	if prefix := cfg.canonical().Prefix; prefix != strings.TrimSpace(prefix) {
		return fmt.Errorf("object_storage.prefix must not start or end with whitespace after path cleaning")
	}
	if cfg.RequestTimeout < 0 {
		return fmt.Errorf("object_storage.request_timeout must not be negative")
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = defaultObjectRequestTimeout
	}
	return nil
}

// openObjectBackend opens the backend for object mode or existing object markers.
// Local mode still needs it to read and prune shards written before a mode change.
func openObjectBackend(ctx context.Context, cfg StoreConfig, db *pebbledb.DB) (shardBackend, error) {
	hasObjects, err := hasObjectMarkers(ctx, db)
	if err != nil {
		return nil, err
	}
	saved, recorded, err := readObjectNamespace(db)
	if err != nil {
		return nil, err
	}
	if hasObjects && !recorded {
		return nil, fmt.Errorf("%w: object markers exist without an object namespace", ErrStoreIntegrity)
	}
	if !hasObjects && cfg.StorageBackend != storageBackendObject {
		return nil, nil
	}
	cfg.Log.Warn("Changing storage_backend only affects new shards. Keep object storage configured and accessible until all object shards are pruned.",
		"storage_backend", cfg.StorageBackend)
	if err := cfg.ObjectStorage.Validate(); err != nil {
		return nil, fmt.Errorf("object storage must remain configured until all object shards are pruned: %w", err)
	}
	if cfg.ObjectStorage.ChainID == "" || cfg.ObjectStorage.ValidatorAddress == "" {
		return nil, fmt.Errorf("chain ID and validator address are required for object storage")
	}
	namespace := cfg.ObjectStorage.canonical()
	mismatch := hasObjects && saved != namespace
	if mismatch {
		cfg.Log.Warn("Object storage namespace changed", "old", saved, "new", namespace,
			"override", cfg.ObjectStorage.OverrideNamespace)
		if !cfg.ObjectStorage.OverrideNamespace {
			return nil, fmt.Errorf("%w: object namespace mismatch; restore the previous configuration or migrate objects before using --override-object-namespace", ErrStoreIntegrity)
		}
	}
	client, err := newObjectClient(ctx, cfg.ObjectStorage)
	if err != nil {
		return nil, err
	}
	if !recorded || saved != namespace {
		if err := saveObjectNamespace(db, namespace); err != nil {
			return nil, err
		}
	}
	backend := newObjectBackend(client, namespace)
	backend.requestTimeout = cfg.ObjectStorage.RequestTimeout
	return backend, nil
}

func newObjectClient(ctx context.Context, cfg ObjectStorageConfig) (*s3.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	httpClient := awshttp.NewBuildableClient().WithTransportOptions(func(tr *http.Transport) {
		tr.ForceAttemptHTTP2 = false
		tr.TLSClientConfig.NextProtos = []string{"http/1.1"}
		tr.TLSClientConfig.ClientSessionCache = tls.NewLRUClientSessionCache(512)
		tr.MaxIdleConns, tr.MaxIdleConnsPerHost = 512, 512
		tr.IdleConnTimeout = 15 * time.Second
		tr.ResponseHeaderTimeout = cfg.RequestTimeout
		tr.WriteBufferSize, tr.ReadBufferSize = 256<<10, 256<<10
	})
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region), config.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("loading AWS configuration: %w", err)
	}
	// The SDK resolves credentials lazily; fail startup if none can be retrieved.
	if _, err := awsConfig.Credentials.Retrieve(ctx); err != nil {
		return nil, fmt.Errorf("loading object storage credentials: %w", err)
	}
	return s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(cfg.Endpoint)
	}), nil
}

// hasObjectMarkers stops at the first valid object marker without reading payloads.
func hasObjectMarkers(ctx context.Context, db *pebbledb.DB) (bool, error) {
	prefix := []byte(shardKeyPrefix)
	iter, err := db.NewIter(&pebbledb.IterOptions{
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
