package fibre

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testObjectStorageConfig() ObjectStorageConfig {
	return ObjectStorageConfig{
		objectNamespace: objectNamespace{
			Endpoint: "https://account.r2.cloudflarestorage.com",
			Bucket:   "fibre-shards", Prefix: "fibre",
		},
		Region: "auto",
	}
}

func TestObjectStorageConfigValidate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*ObjectStorageConfig)
	}{
		{"endpoint missing", func(c *ObjectStorageConfig) { c.Endpoint = "" }},
		{"endpoint relative", func(c *ObjectStorageConfig) { c.Endpoint = "/r2" }},
		{"endpoint scheme", func(c *ObjectStorageConfig) { c.Endpoint = "ftp://r2.example" }},
		{"endpoint malformed", func(c *ObjectStorageConfig) { c.Endpoint = "https://%" }},
		{"endpoint credentials", func(c *ObjectStorageConfig) { c.Endpoint = "https://user:secret@r2.example" }},
		{"endpoint query", func(c *ObjectStorageConfig) { c.Endpoint += "?query=1" }},
		{"endpoint fragment", func(c *ObjectStorageConfig) { c.Endpoint += "#fragment" }},
		{"region", func(c *ObjectStorageConfig) { c.Region = " " }},
		{"bucket", func(c *ObjectStorageConfig) { c.Bucket = " " }},
		{"prefix", func(c *ObjectStorageConfig) { c.Prefix = " " }},
		{"prefix slashes", func(c *ObjectStorageConfig) { c.Prefix = "///" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testObjectStorageConfig()
			tc.modify(&cfg)
			require.Error(t, cfg.Validate())
		})
	}
	cfg := testObjectStorageConfig()
	require.NoError(t, cfg.Validate())
	cfg.Endpoint = "http://localhost:9000"
	require.NoError(t, cfg.Validate())
}

func TestObjectStorageConfigNormalisesWhitespace(t *testing.T) {
	cfg := testObjectStorageConfig()
	want := cfg
	cfg.Region = " \tauto\n"
	cfg.Bucket = " fibre-shards "
	cfg.Prefix = " fibre "
	require.NoError(t, cfg.Validate())
	require.Equal(t, want, cfg)
}

func TestStoreRejectsMissingObjectCredentials(t *testing.T) {
	clearAWSCredentials(t)
	cfg := DefaultStoreConfig()
	cfg.Path = t.TempDir()
	store, err := NewStore(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, store.db.Set(shardKey(Commitment{}, []byte{1}), encodeShardMarkerForBackend(objectBackendTag, 1), pebbledb.Sync))
	cfg.ObjectStorage = testObjectStorageConfig()
	cfg.ObjectStorage.ChainID, cfg.ObjectStorage.ValidatorAddress = "test-chain", "test-validator"
	namespace, err := json.Marshal(cfg.ObjectStorage.canonical())
	require.NoError(t, err)
	require.NoError(t, store.db.Set([]byte(objectNamespaceKey), namespace, pebbledb.Sync))
	require.NoError(t, store.Close())
	for _, mode := range []string{"local", "object"} {
		t.Run(mode, func(t *testing.T) {
			cfg.StorageBackend = mode
			store, err := NewStore(t.Context(), cfg)
			if store != nil {
				require.NoError(t, store.Close())
			}
			require.ErrorContains(t, err, "credentials")
		})
	}
}

func clearAWSCredentials(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_ACCESS_KEY", "AWS_SECRET_KEY", "AWS_SESSION_TOKEN", "AWS_PROFILE", "AWS_DEFAULT_PROFILE",
		"AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_ROLE_ARN", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "AWS_CONTAINER_CREDENTIALS_FULL_URI",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "missing"))
}

// TestStoreCancelsCredentialLookup cancels startup while the credential provider is still waiting.
func TestStoreCancelsCredentialLookup(t *testing.T) {
	clearAWSCredentials(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cancel()
		<-release
		_, _ = io.WriteString(w, `{"AccessKeyId":"test-key","SecretAccessKey":"test-secret","Token":"test-token","Expiration":"2100-01-01T00:00:00Z"}`)
	}))
	defer server.Close()
	defer close(release)
	t.Setenv("AWS_CONTAINER_CREDENTIALS_FULL_URI", server.URL)
	cfg := DefaultStoreConfig()
	cfg.Path = t.TempDir()
	cfg.StorageBackend = "object"
	cfg.ObjectStorage = testObjectStorageConfig()
	cfg.ObjectStorage.ChainID, cfg.ObjectStorage.ValidatorAddress = "test-chain", "test-validator"
	store, err := NewStore(ctx, cfg)
	if store != nil {
		require.NoError(t, store.Close())
	}
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.ErrorIs(t, err, context.Canceled)
}

func TestStoreConfigValidateBackend(t *testing.T) {
	cfg := StoreConfig{Path: t.TempDir()}
	require.NoError(t, cfg.Validate())
	require.Equal(t, "local", cfg.StorageBackend)
	cfg.StorageBackend = "unknown"
	require.ErrorContains(t, cfg.Validate(), "storage_backend")
	cfg.StorageBackend = "object"
	require.ErrorContains(t, cfg.Validate(), "object_storage.endpoint")
	cfg.ObjectStorage = testObjectStorageConfig()
	require.NoError(t, cfg.Validate())
	_, err := NewStore(t.Context(), cfg)
	require.ErrorContains(t, err, "chain ID and validator address")
	cfg.ObjectStorage.ChainID = "test-chain"
	_, err = NewStore(t.Context(), cfg)
	require.ErrorContains(t, err, "chain ID and validator address")
}

// TestStoreConfiguredBackendSwitch checks reads and pruning across local-to-object-to-local restarts.
// It also checks SDK signing, object keys, and the required configuration for retained object markers.
func TestStoreConfiguredBackendSwitch(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))
	var mu sync.Mutex
	rejectWrites := true
	objects := make(map[string][]byte)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		assert.Contains(t, r.Header.Get("Authorization"), "Credential=test-key/")
		switch r.Method {
		case http.MethodPut:
			if rejectWrites {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			assert.Equal(t, "*", r.Header.Get("If-None-Match"))
			data, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.Equal(t, int64(len(data)), r.ContentLength)
			objects[r.URL.Path] = data
		case http.MethodGet, http.MethodHead:
			data, ok := objects[r.URL.Path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if r.Method == http.MethodGet {
				_, _ = w.Write(data)
			}
		case http.MethodPost:
			assert.True(t, r.URL.Query().Has("delete"))
			var request struct {
				Keys []string `xml:"Object>Key"`
			}
			if err := xml.NewDecoder(r.Body).Decode(&request); !assert.NoError(t, err) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for _, key := range request.Keys {
				delete(objects, r.URL.Path+"/"+key)
			}
			_, _ = w.Write([]byte("<DeleteResult/>"))
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	var logs bytes.Buffer
	cfg := DefaultStoreConfig()
	cfg.Log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg.Path = t.TempDir()
	cfg.ObjectStorage = testObjectStorageConfig()
	cfg.ObjectStorage.Endpoint = server.URL
	cfg.ObjectStorage.Region = " auto "
	cfg.ObjectStorage.Bucket = " fibre-shards "
	cfg.ObjectStorage.Prefix = " fibre "
	cfg.ObjectStorage.ChainID = "test-chain"
	cfg.ObjectStorage.ValidatorAddress = "test-validator"
	shard := &types.BlobShard{Rows: []*types.BlobRow{{Index: 1, Data: []byte("data")}}}
	pruneAt := time.Unix(60, 0)
	promise := &PaymentPromise{
		ChainID: cfg.ObjectStorage.ChainID, SignerKey: secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey),
		Commitment: generateCommitment(), CreationTimestamp: pruneAt, Signature: []byte{1},
	}
	localCommitment := promise.Commitment
	store, err := NewStore(t.Context(), cfg)
	require.NoError(t, err)
	require.Empty(t, logs.String())
	require.NoError(t, store.Put(t.Context(), promise, shard, pruneAt))
	require.NoError(t, store.Close())

	cfg.StorageBackend = "object"
	store, err = NewStore(t.Context(), cfg)
	require.NoError(t, err)
	_, recorded, err := readObjectNamespace(store.db)
	require.NoError(t, err)
	require.True(t, recorded, "startup must save the namespace before accepting uploads")
	data, closer, err := store.db.Get([]byte(objectNamespaceKey))
	require.NoError(t, err)
	// Equivalent JSON formatting makes unnecessary rewrites observable.
	namespaceData := append(bytes.Clone(data), '\n')
	require.NoError(t, closer.Close())
	require.NoError(t, store.db.Set([]byte(objectNamespaceKey), namespaceData, pebbledb.Sync))
	require.NoError(t, store.Close())
	store, err = NewStore(t.Context(), cfg)
	require.NoError(t, err)
	require.Contains(t, logs.String(), "level=WARN")
	require.Contains(t, logs.String(), "Changing storage_backend only affects new shards")
	logs.Reset()
	got, err := store.Get(t.Context(), localCommitment)
	require.NoError(t, err)
	require.Equal(t, shard, got)
	promise.Commitment[0]++
	require.Error(t, store.Put(t.Context(), promise, shard, pruneAt))
	_, recorded, err = readObjectNamespace(store.db)
	require.NoError(t, err)
	require.True(t, recorded, "failed uploads must preserve the startup namespace")
	mu.Lock()
	rejectWrites = false
	mu.Unlock()
	require.NoError(t, store.Put(t.Context(), promise, shard, pruneAt))
	data, closer, err = store.db.Get([]byte(objectNamespaceKey))
	require.NoError(t, err)
	require.Equal(t, namespaceData, data, "matching restarts and uploads must not rewrite the namespace")
	require.NoError(t, closer.Close())
	promiseHash, err := promise.Hash()
	require.NoError(t, err)
	mu.Lock()
	assert.Contains(t, objects, "/fibre-shards/fibre/test-chain/test-validator/shards/"+promise.Commitment.String()+"-"+hex.EncodeToString(promiseHash))
	mu.Unlock()
	require.NoError(t, store.Close())

	for _, mode := range []string{"object", "local"} {
		for _, tc := range []struct {
			name   string
			modify func(*ObjectStorageConfig)
		}{
			{"endpoint", func(c *ObjectStorageConfig) { c.Endpoint += "/other" }},
			{"bucket", func(c *ObjectStorageConfig) { c.Bucket = "other-bucket" }},
			{"prefix", func(c *ObjectStorageConfig) { c.Prefix = "other-prefix" }},
			{"chain", func(c *ObjectStorageConfig) { c.ChainID = "other-chain" }},
			{"validator", func(c *ObjectStorageConfig) { c.ValidatorAddress = "other-validator" }},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				changed := cfg
				changed.StorageBackend = mode
				tc.modify(&changed.ObjectStorage)
				opened, err := NewStore(t.Context(), changed)
				if opened != nil {
					require.NoError(t, opened.Close())
				}
				require.ErrorIs(t, err, ErrStoreIntegrity)
				require.Contains(t, logs.String(), "Object storage namespace changed")
				require.Contains(t, logs.String(), "old=")
				require.Contains(t, logs.String(), "new=")
			})
		}
	}

	cfg.StorageBackend = "local"
	retainedConfig := cfg.ObjectStorage
	cfg.ObjectStorage = ObjectStorageConfig{}
	_, err = NewStore(t.Context(), cfg)
	require.ErrorContains(t, err, "object_storage.endpoint")
	require.ErrorContains(t, err, "object storage must remain configured until all object shards are pruned")
	logs.Reset()
	cfg.ObjectStorage = retainedConfig
	cfg.ObjectStorage.Prefix = " /fibre/./ "
	store, err = NewStore(t.Context(), cfg)
	require.NoError(t, err)
	require.Contains(t, logs.String(), "level=WARN")
	require.Contains(t, logs.String(), "Keep object storage configured and accessible until all object shards are pruned")
	require.Equal(t, localBackendTag, store.shards.primary.backendTag())
	got, err = store.Get(t.Context(), promise.Commitment)
	require.NoError(t, err)
	require.Equal(t, shard, got)
	has, err := store.Has(t.Context(), promise.Commitment, promiseHash)
	require.NoError(t, err)
	require.True(t, has)
	require.NoError(t, store.Close())

	// The operator moves the objects before accepting the new namespace.
	mu.Lock()
	for key, data := range objects {
		delete(objects, key)
		objects[strings.Replace(key, "/fibre/", "/migrated/", 1)] = data
	}
	mu.Unlock()
	cfg.ObjectStorage.Prefix = "migrated"
	cfg.ObjectStorage.OverrideNamespace = true
	logs.Reset()
	store, err = NewStore(t.Context(), cfg)
	require.NoError(t, err)
	require.Contains(t, logs.String(), "override=true")
	require.NoError(t, store.Close())
	cfg.ObjectStorage.OverrideNamespace = false
	store, err = NewStore(t.Context(), cfg)
	require.NoError(t, err)
	got, err = store.Get(t.Context(), promise.Commitment)
	require.NoError(t, err)
	require.Equal(t, shard, got)
	pruned, freed, err := store.PruneBefore(t.Context(), pruneAt.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, 2, pruned)
	require.Equal(t, 2*shardBinarySize(shard), freed)
	mu.Lock()
	assert.Empty(t, objects)
	mu.Unlock()
	require.NoError(t, store.Close())

	logs.Reset()
	cfg.ObjectStorage = ObjectStorageConfig{}
	store, err = NewStore(t.Context(), cfg)
	require.NoError(t, err)
	require.Nil(t, store.shards.secondary)
	require.Empty(t, logs.String())
	require.NoError(t, store.Close())

	cfg.StorageBackend = "object"
	cfg.ObjectStorage = retainedConfig
	cfg.ObjectStorage.Prefix = "after-pruning"
	store, err = NewStore(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, store.Put(t.Context(), promise, shard, pruneAt))
	require.NoError(t, store.Close())
	store, err = NewStore(t.Context(), cfg)
	require.NoError(t, err)
	got, err = store.Get(t.Context(), promise.Commitment)
	require.NoError(t, err)
	require.Equal(t, shard, got)
	require.NoError(t, store.Close())
}

func TestStoreRejectsInvalidObjectNamespace(t *testing.T) {
	for _, data := range []string{"missing", "", "null", "{}", "{", `{"endpoint":"https://example.com","bucket":"bucket","prefix":"prefix","chain_id":"chain"}`} {
		t.Run(data, func(t *testing.T) {
			cfg := DefaultStoreConfig()
			cfg.Path = t.TempDir()
			store, err := NewStore(t.Context(), cfg)
			require.NoError(t, err)
			require.NoError(t, store.db.Set(shardKey(Commitment{}, []byte{1}), encodeShardMarkerForBackend(objectBackendTag, 1), pebbledb.Sync))
			if data != "missing" {
				require.NoError(t, store.db.Set([]byte(objectNamespaceKey), []byte(data), pebbledb.Sync))
			}
			require.NoError(t, store.Close())
			cfg.ObjectStorage = testObjectStorageConfig()
			cfg.ObjectStorage.ChainID, cfg.ObjectStorage.ValidatorAddress = "test-chain", "test-validator"
			for _, mode := range []string{"local", "object"} {
				cfg.StorageBackend = mode
				for _, override := range []bool{false, true} {
					cfg.ObjectStorage.OverrideNamespace = override
					_, err := NewStore(t.Context(), cfg)
					require.ErrorIs(t, err, ErrStoreIntegrity)
				}
			}
		})
	}
}

// TestStoreLocalDoesNotLoadAWSConfig checks that local stores without object markers ignore an invalid AWS profile.
// Object mode must load the profile and report its error.
func TestStoreLocalDoesNotLoadAWSConfig(t *testing.T) {
	t.Setenv("AWS_PROFILE", "missing-profile")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "missing"))
	cfg := DefaultStoreConfig()
	cfg.Path = t.TempDir()
	store, err := NewStore(t.Context(), cfg)
	require.NoError(t, err)
	require.Nil(t, store.shards.secondary)
	// Legacy and invalid markers do not identify an object backend.
	require.NoError(t, store.db.Set(shardKey(Commitment{}, []byte{1}), nil, pebbledb.Sync))
	require.NoError(t, store.db.Set(shardKey(Commitment{}, []byte{2}), []byte{1}, pebbledb.Sync))
	require.NoError(t, store.Close())
	store, err = NewStore(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, store.Close())
	cfg.StorageBackend = "object"
	cfg.ObjectStorage = testObjectStorageConfig()
	cfg.ObjectStorage.ChainID, cfg.ObjectStorage.ValidatorAddress = "test-chain", "test-validator"
	_, err = NewStore(t.Context(), cfg)
	require.ErrorContains(t, err, "loading AWS configuration")
}
