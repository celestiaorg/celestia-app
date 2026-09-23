package fibre

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
)

func TestHashFirstObjectKey(t *testing.T) {
	namespace := objectNamespace{Bucket: "bucket", Prefix: "fibre", ChainID: "chain", ValidatorAddress: "validator"}
	legacy := newObjectBackend(nil, namespace)
	hashed := newObjectBackend(nil, namespace)
	hashed.hashFirst = true
	commitment := Commitment{}
	hash := []byte{1, 2, 3}
	oldKey := legacy.objectKey(commitment, hash)
	key := hashed.objectKey(commitment, hash)
	require.Equal(t, key, hashed.objectKey(commitment, hash))
	require.Equal(t, oldKey, key[3:])
	require.Equal(t, byte('/'), key[2])
	_, err := hex.DecodeString(key[:2])
	require.NoError(t, err)
	require.Equal(t, objectBackendTag, legacy.backendTag())
	require.Equal(t, hashedObjectBackendTag, hashed.backendTag())
	prefixes := make(map[string]bool)
	for i := uint64(0); i < 4096; i++ {
		key := hashed.objectKey(commitment, binary.BigEndian.AppendUint64(nil, i))
		prefixes[strings.SplitN(key, "/", 2)[0]] = true
	}
	require.Greater(t, len(prefixes), 240, "sequential promises should spread across the 256 leading prefixes")
}

func TestHashFirstMixedRouting(t *testing.T) {
	shard := &types.BlobShard{Rows: []*types.BlobRow{{Index: 1, Data: []byte("data")}}}
	var encoded bytes.Buffer
	require.NoError(t, writeShardBinary(&encoded, shard))
	namespace := objectNamespace{Bucket: "legacy", Prefix: "fibre", ChainID: "chain", ValidatorAddress: "validator"}
	legacy := newObjectBackend(nil, namespace)
	namespace.Bucket = "hashed"
	hashed := newObjectBackend(nil, namespace)
	hashed.hashFirst = true
	namespace.Bucket = "next"
	next := newObjectBackend(nil, namespace)
	next.hashFirst, next.hashedTag = true, nextHashedObjectBackendTag
	promiseBackend := newObjectBackend(nil, namespace)
	promiseBackend.namespace.Bucket = "promise"
	promiseBackend.hashFirst, promiseBackend.hashedTag = true, promiseHashObjectBackendTag
	commitment := Commitment{}
	hash := []byte{1}
	seen := make(map[string]int)
	for _, backend := range []*objectBackend{legacy, hashed, next, promiseBackend} {
		backend := backend
		check := func(operation, bucket, key string) {
			require.Equal(t, backend.namespace.Bucket, bucket)
			require.Equal(t, backend.objectKey(commitment, hash), key)
			seen[operation+bucket]++
		}
		backend.client = &s3ObjectClientStub{
			getObject: func(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
				check("get", aws.ToString(in.Bucket), aws.ToString(in.Key))
				return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(encoded.Bytes()))}, nil
			},
			headObject: func(_ context.Context, in *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
				check("head", aws.ToString(in.Bucket), aws.ToString(in.Key))
				return &s3.HeadObjectOutput{}, nil
			},
			deleteObject: func(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
				check("delete", aws.ToString(in.Bucket), aws.ToString(in.Key))
				return &s3.DeleteObjectOutput{}, nil
			},
			deleteObjects: func(_ context.Context, in *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
				require.Len(t, in.Delete.Objects, 1)
				check("batch", aws.ToString(in.Bucket), aws.ToString(in.Delete.Objects[0].Key))
				return &s3.DeleteObjectsOutput{}, nil
			},
		}
	}
	storage := &routedStorage{primary: promiseBackend, secondary: legacy, tertiary: hashed, quaternary: next}
	var marked []markedShard
	for _, backend := range []*objectBackend{legacy, hashed, next, promiseBackend} {
		marker := encodeShardMarkerForBackend(backend.backendTag(), int64(encoded.Len()))
		got, err := storage.Get(t.Context(), marker, commitment, hash)
		require.NoError(t, err)
		require.Equal(t, shard, got)
		has, err := storage.Has(t.Context(), marker, commitment, hash)
		require.NoError(t, err)
		require.True(t, has)
		require.NoError(t, storage.Delete(t.Context(), marker, commitment, hash))
		marked = append(marked, markedShard{id: shardID{commitment: commitment, promiseHash: hash}, marker: marker})
	}
	successful, err := storage.DeleteBatch(t.Context(), marked)
	require.NoError(t, err)
	require.ElementsMatch(t, []int{0, 1, 2, 3}, successful)
	for _, bucket := range []string{"legacy", "hashed", "next", "promise"} {
		for _, operation := range []string{"get", "head", "delete", "batch"} {
			require.Equal(t, 1, seen[operation+bucket], operation+bucket)
		}
	}
}

func TestHashFirstNamespaceRestart(t *testing.T) {
	clearAWSCredentials(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "test-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	cfg := DefaultStoreConfig()
	cfg.Path = t.TempDir()
	cfg.StorageBackend = "object"
	cfg.ObjectStorage = testObjectStorageConfig()
	cfg.ObjectStorage.ChainID = "test-chain"
	cfg.ObjectStorage.ValidatorAddress = "test-validator"
	store, err := NewStore(t.Context(), cfg)
	require.NoError(t, err)
	require.Equal(t, objectBackendTag, store.shards.primary.backendTag())
	require.NoError(t, store.Close())
	cfg.ObjectStorage.HashFirstBucket = "hash-first-shards"
	for i := range 2 {
		store, err = NewStore(t.Context(), cfg)
		require.NoError(t, err)
		require.Equal(t, hashedObjectBackendTag, store.shards.primary.backendTag())
		_, err = store.shards.backend(objectBackendTag)
		require.NoError(t, err)
		_, err = store.shards.backend(localBackendTag)
		require.NoError(t, err)
		if i == 1 {
			require.NoError(t, store.db.Set(shardKey(Commitment{}, []byte{1}), encodeShardMarkerForBackend(hashedObjectBackendTag, 1), pebbledb.Sync))
		}
		require.NoError(t, store.Close())
	}
	cfg.ObjectStorage.HashFirstBucket = "wrong-bucket"
	store, err = NewStore(t.Context(), cfg)
	if store != nil {
		require.NoError(t, store.Close())
	}
	require.ErrorIs(t, err, ErrStoreIntegrity)
	for _, mode := range []string{"object", "local"} {
		t.Run(mode+" missing hash-first bucket", func(t *testing.T) {
			missing := cfg
			missing.StorageBackend = mode
			missing.ObjectStorage.HashFirstBucket = ""
			opened, err := NewStore(t.Context(), missing)
			if opened != nil {
				require.NoError(t, opened.Close())
			}
			require.ErrorIs(t, err, ErrStoreIntegrity)
			require.ErrorContains(t, err, "hash_first_bucket")
		})
	}
}

func TestHashFirstPutPreservesLegacyMarker(t *testing.T) {
	store := newMarkerTestStore(t)
	legacy := newObjectBackend(&s3ObjectClientStub{
		putObject: func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			return &s3.PutObjectOutput{}, nil
		},
	}, objectNamespace{Bucket: "legacy"})
	store.shards = newRoutedStorage(legacy, store.shards.primary)
	promise := &PaymentPromise{ChainID: "test-chain", SignerKey: secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey), Commitment: generateCommitment(), CreationTimestamp: time.Unix(1, 0), Signature: []byte{1}}
	shard := &types.BlobShard{Rows: []*types.BlobRow{{Index: 1, Data: []byte("data")}}}
	require.NoError(t, store.Put(t.Context(), promise, shard, promise.CreationTimestamp))
	hashed := newObjectBackend(&s3ObjectClientStub{
		putObject: func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			t.Fatal("existing legacy shard must not be redirected to hashed storage")
			return nil, nil
		},
	}, objectNamespace{Bucket: "hashed"})
	hashed.hashFirst = true
	store.shards.primary, store.shards.secondary = hashed, legacy
	require.NoError(t, store.Put(t.Context(), promise, shard, promise.CreationTimestamp))
	hash, err := promise.Hash()
	require.NoError(t, err)
	data, closer, err := store.db.Get(shardKey(promise.Commitment, hash))
	require.NoError(t, err)
	defer closer.Close()
	tag, _, err := decodeShardMarkerBackend(data)
	require.NoError(t, err)
	require.Equal(t, objectBackendTag, tag)
}

func TestHashFirstMixedBatchFailureIsolation(t *testing.T) {
	for _, failedTag := range []shardBackendTag{objectBackendTag, hashedObjectBackendTag, nextHashedObjectBackendTag, promiseHashObjectBackendTag} {
		t.Run(string(rune('0'+failedTag)), func(t *testing.T) {
			legacy := newObjectBackend(nil, objectNamespace{Bucket: "legacy"})
			hashed := newObjectBackend(nil, objectNamespace{Bucket: "hashed"})
			hashed.hashFirst = true
			next := newObjectBackend(nil, objectNamespace{Bucket: "next"})
			next.hashFirst, next.hashedTag = true, nextHashedObjectBackendTag
			promiseBackend := newObjectBackend(nil, objectNamespace{Bucket: "promise"})
			promiseBackend.hashFirst, promiseBackend.hashedTag = true, promiseHashObjectBackendTag
			var shards []markedShard
			var wantSuccessful []int
			for _, backend := range []*objectBackend{legacy, hashed, next, promiseBackend} {
				backend := backend
				backend.client = &s3ObjectClientStub{deleteObjects: func(_ context.Context, in *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
					require.Equal(t, backend.namespace.Bucket, aws.ToString(in.Bucket))
					require.Len(t, in.Delete.Objects, 2)
					if backend.backendTag() == failedTag {
						return &s3.DeleteObjectsOutput{Errors: []s3types.Error{{Key: in.Delete.Objects[0].Key, Code: aws.String("InternalError")}}}, nil
					}
					return &s3.DeleteObjectsOutput{}, nil
				}}
				for i := range 2 {
					if backend.backendTag() != failedTag || i == 1 {
						wantSuccessful = append(wantSuccessful, len(shards))
					}
					shards = append(shards, markedShard{id: shardID{promiseHash: []byte{byte(i)}}, marker: encodeShardMarkerForBackend(backend.backendTag(), 1)})
				}
			}
			storage := &routedStorage{primary: promiseBackend, secondary: legacy, tertiary: hashed, quaternary: next}
			successful, err := storage.DeleteBatch(t.Context(), shards)
			require.Error(t, err)
			require.IsType(t, &partialDeleteError{}, err)
			require.ElementsMatch(t, wantSuccessful, successful)
		})
	}
}

func TestHashNextNamespaceRestart(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	cfg := DefaultStoreConfig()
	cfg.Path = t.TempDir()
	cfg.StorageBackend = storageBackendObject
	cfg.ObjectStorage = testObjectStorageConfig()
	cfg.ObjectStorage.ChainID, cfg.ObjectStorage.ValidatorAddress = "chain", "validator"
	cfg.ObjectStorage.HashFirstBucket, cfg.ObjectStorage.HashFirstBucketNext = "hashed", "next"
	for range 2 {
		store, err := NewStore(t.Context(), cfg)
		require.NoError(t, err)
		require.Equal(t, nextHashedObjectBackendTag, store.shards.primary.backendTag())
		for _, tag := range []shardBackendTag{localBackendTag, objectBackendTag, hashedObjectBackendTag, nextHashedObjectBackendTag} {
			_, err := store.shards.backend(tag)
			require.NoError(t, err)
			require.NoError(t, store.db.Set(shardKey(Commitment{}, []byte{byte(tag)}), encodeShardMarkerForBackend(tag, 1), pebbledb.Sync))
		}
		require.NoError(t, store.Close())
	}
	for _, mode := range []string{storageBackendObject, storageBackendLocal} {
		for _, bucket := range []string{"", "wrong"} {
			changed := cfg
			changed.StorageBackend = mode
			changed.ObjectStorage.HashFirstBucketNext = bucket
			store, err := NewStore(t.Context(), changed)
			if store != nil {
				require.NoError(t, store.Close())
			}
			require.ErrorIs(t, err, ErrStoreIntegrity)
		}
	}
}

func TestHashNextPutPreservesPreviousMarker(t *testing.T) {
	store := newMarkerTestStore(t)
	previous := newObjectBackend(&s3ObjectClientStub{putObject: func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
		return &s3.PutObjectOutput{}, nil
	}}, objectNamespace{Bucket: "hashed"})
	previous.hashFirst = true
	store.shards = newRoutedStorage(previous, store.shards.primary)
	promise := &PaymentPromise{ChainID: "chain", SignerKey: secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey), Commitment: generateCommitment(), CreationTimestamp: time.Unix(1, 0), Signature: []byte{1}}
	shard := &types.BlobShard{Rows: []*types.BlobRow{{Data: []byte("data")}}}
	require.NoError(t, store.Put(t.Context(), promise, shard, promise.CreationTimestamp))
	next := newObjectBackend(&s3ObjectClientStub{putObject: func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
		t.Fatal("old identity redirected")
		return nil, nil
	}}, objectNamespace{Bucket: "next"})
	next.hashFirst, next.hashedTag = true, nextHashedObjectBackendTag
	store.shards.primary, store.shards.tertiary = next, previous
	require.NoError(t, store.Put(t.Context(), promise, shard, promise.CreationTimestamp))
	hash, err := promise.Hash()
	require.NoError(t, err)
	marker, closer, err := store.db.Get(shardKey(promise.Commitment, hash))
	require.NoError(t, err)
	defer closer.Close()
	tag, _, err := decodeShardMarkerBackend(marker)
	require.NoError(t, err)
	require.Equal(t, hashedObjectBackendTag, tag)
}

func TestPromiseHashKeyLayout(t *testing.T) {
	namespace := objectNamespace{Bucket: "bucket", Prefix: "namespace", ChainID: "chain", ValidatorAddress: "validator"}
	legacy := newObjectBackend(nil, namespace)
	backend := newObjectBackend(nil, namespace)
	backend.hashFirst, backend.hashedTag = true, promiseHashObjectBackendTag
	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = byte(i)
	}
	want := hex.EncodeToString(hash) + "/" + legacy.objectKey(Commitment{}, hash)
	require.Equal(t, want, backend.objectKey(Commitment{}, hash))
	require.Equal(t, byte('/'), want[64])
}

func TestPromiseHashNamespaceRestart(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	cfg := DefaultStoreConfig()
	cfg.Path = t.TempDir()
	cfg.StorageBackend = storageBackendObject
	cfg.ObjectStorage = testObjectStorageConfig()
	cfg.ObjectStorage.ChainID, cfg.ObjectStorage.ValidatorAddress = "chain", "validator"
	cfg.ObjectStorage.HashFirstBucket, cfg.ObjectStorage.HashFirstBucketNext = "hashed", "next"
	cfg.ObjectStorage.PromiseHashKeys = true
	for range 2 {
		store, err := NewStore(t.Context(), cfg)
		require.NoError(t, err)
		require.Equal(t, promiseHashObjectBackendTag, store.shards.primary.backendTag())
		for _, tag := range []shardBackendTag{localBackendTag, objectBackendTag, hashedObjectBackendTag, nextHashedObjectBackendTag, promiseHashObjectBackendTag} {
			_, err = store.shards.backend(tag)
			require.NoError(t, err)
			require.NoError(t, store.db.Set(shardKey(Commitment{}, []byte{byte(tag)}), encodeShardMarkerForBackend(tag, 1), pebbledb.Sync))
		}
		require.NoError(t, store.Close())
	}
	for _, mode := range []string{storageBackendLocal, storageBackendObject} {
		for _, change := range []string{"disabled", "bucket"} {
			changed := cfg
			changed.StorageBackend = mode
			if change == "disabled" {
				changed.ObjectStorage.PromiseHashKeys = false
			} else {
				changed.ObjectStorage.HashFirstBucketNext = "wrong"
			}
			store, err := NewStore(t.Context(), changed)
			if store != nil {
				require.NoError(t, store.Close())
			}
			require.ErrorIs(t, err, ErrStoreIntegrity)
		}
	}
}
