package fibre

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
)

type packedTestS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    int
	fail    bool
}

func newPackedTestBackend(t *testing.T, size int) (*packedBackend, *packedTestS3) {
	t.Helper()
	db, err := pebbledb.Open(t.TempDir(), &pebbledb.Options{})
	require.NoError(t, err)
	mock := &packedTestS3{objects: make(map[string][]byte)}
	client := &s3ObjectClientStub{
		putObject: func(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			data, err := io.ReadAll(in.Body)
			if err != nil {
				return nil, err
			}
			mock.mu.Lock()
			defer mock.mu.Unlock()
			mock.puts++
			if mock.fail {
				return nil, errors.New("failed PUT")
			}
			mock.objects[aws.ToString(in.Key)] = data
			return &s3.PutObjectOutput{}, nil
		},
		getObject: func(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			mock.mu.Lock()
			defer mock.mu.Unlock()
			data := mock.objects[aws.ToString(in.Key)]
			var start, end int
			_, err := fmt.Sscanf(aws.ToString(in.Range), "bytes=%d-%d", &start, &end)
			if err != nil {
				return nil, err
			}
			if start < 0 || end >= len(data) {
				return nil, errors.New("invalid range")
			}
			return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(data[start : end+1]))}, nil
		},
		deleteObject: func(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			mock.mu.Lock()
			defer mock.mu.Unlock()
			delete(mock.objects, aws.ToString(in.Key))
			return &s3.DeleteObjectOutput{}, nil
		},
		headObject: func(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			return &s3.HeadObjectOutput{}, nil
		},
	}
	object := newObjectBackend(client, objectNamespace{Bucket: "bucket", Prefix: "run/validator-042", ChainID: "chain", ValidatorAddress: "address"})
	b := newPackedBackend(object, db, size)
	t.Cleanup(func() { b.Close(); require.NoError(t, db.Close()) })
	return b, mock
}
func TestPackedFullBatchRangeAndPrune(t *testing.T) {
	b, mock := newPackedTestBackend(t, 16)
	shards := make([]*types.BlobShard, 16)
	errs := make(chan error, 16)
	for i := range shards {
		i := i
		shards[i] = &types.BlobShard{Rlcs: []byte{byte(i)}, Rows: []*types.BlobRow{{Data: bytes.Repeat([]byte{byte(i)}, 100)}}}
		go func() { errs <- b.Put(t.Context(), Commitment{}, []byte{3, byte(i)}, shards[i]) }()
	}
	for range shards {
		require.NoError(t, <-errs)
	}
	require.Equal(t, 1, mock.puts)
	require.Len(t, mock.objects, 1)
	for key := range mock.objects {
		require.True(t, strings.HasPrefix(key, "03/run/validator-042/"))
	}
	for i, shard := range shards {
		got, err := b.Get(t.Context(), Commitment{}, []byte{3, byte(i)})
		require.NoError(t, err)
		require.Equal(t, shard, got)
	}
	require.NoError(t, b.Put(t.Context(), Commitment{}, []byte{3, 0}, shards[0]))
	require.Equal(t, 1, mock.puts)
	for i := 0; i < 15; i++ {
		require.NoError(t, b.Delete(t.Context(), Commitment{}, []byte{3, byte(i)}))
		require.Len(t, mock.objects, 1)
	}
	got, err := b.Get(t.Context(), Commitment{}, []byte{3, 15})
	require.NoError(t, err)
	require.Equal(t, shards[15], got)
	require.NoError(t, b.Delete(t.Context(), Commitment{}, []byte{3, 15}))
	require.Empty(t, mock.objects)
}
func TestPackedFailedPutRecovery(t *testing.T) {
	b, mock := newPackedTestBackend(t, 1)
	shard := &types.BlobShard{Rows: []*types.BlobRow{}, Rlcs: []byte("data")}
	start := time.Now()
	require.NoError(t, b.Put(t.Context(), Commitment{}, []byte{7}, shard))
	require.Less(t, time.Since(start), time.Second)
	require.NoError(t, b.db.Set(shardKey(Commitment{}, []byte{7}), encodeShardMarkerForBackend(packedObjectBackendTag, shardBinarySize(shard)), pebbledb.Sync))
	mock.fail = true
	require.ErrorContains(t, b.Put(t.Context(), Commitment{}, []byte{15}, shard), "failed PUT")
	mock.fail = false
	require.NoError(t, b.recover(t.Context()))
	require.Len(t, mock.objects, 1)
	reopened := newPackedBackend(b.object, b.db, 16)
	require.NoError(t, reopened.recover(t.Context()))
	got, err := reopened.Get(t.Context(), Commitment{}, []byte{7})
	require.NoError(t, err)
	require.Equal(t, shard, got)
	reopened.Close()
	// A completed S3 upload with no final Store marker is an unacknowledged orphan.
	require.NoError(t, b.db.Delete(shardKey(Commitment{}, []byte{7}), pebbledb.Sync))
	require.NoError(t, b.recover(t.Context()))
	require.Empty(t, mock.objects)
}
func TestPackedEightGroupsAndCancellation(t *testing.T) {
	b, mock := newPackedTestBackend(t, 1)
	shard := &types.BlobShard{Rows: []*types.BlobRow{}, Rlcs: []byte("data")}
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		i := i
		go func() { errs <- b.Put(t.Context(), Commitment{}, []byte{byte(i)}, shard) }()
	}
	for i := 0; i < 8; i++ {
		require.NoError(t, <-errs)
	}
	require.Equal(t, 8, mock.puts)
	groups := map[string]bool{}
	for key := range mock.objects {
		groups[key[:2]] = true
	}
	require.Len(t, groups, 8)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, b.Put(ctx, Commitment{}, []byte{10}, shard), context.Canceled)
}

func TestPackedStoreCommitRestartAndPrune(t *testing.T) {
	b, mock := newPackedTestBackend(t, 16)
	store := &Store{db: b.db, log: slog.Default(), shards: &routedStorage{primary: b.object, packed: b}}
	promise := &PaymentPromise{ChainID: "chain", SignerKey: secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey), Commitment: generateCommitment(), CreationTimestamp: time.Unix(1, 0), Signature: []byte{1}}
	shard := &types.BlobShard{Rows: []*types.BlobRow{{Data: []byte("payload")}}, Rlcs: []byte("rlc")}
	h0, err := promise.Hash()
	require.NoError(t, err)
	promises := []*PaymentPromise{promise}
	for i := 0; len(promises) < 16; i++ {
		candidate := &PaymentPromise{ChainID: promise.ChainID, SignerKey: promise.SignerKey, Commitment: promise.Commitment, CreationTimestamp: promise.CreationTimestamp, Signature: []byte{2, byte(i), byte(i >> 8)}}
		h, err := candidate.Hash()
		require.NoError(t, err)
		if h[0]&7 == h0[0]&7 {
			promises = append(promises, candidate)
		}
	}
	errs := make(chan error, 16)
	for _, p := range promises {
		go func() { errs <- store.Put(t.Context(), p, shard, time.Unix(100, 0)) }()
	}
	for range promises {
		require.NoError(t, <-errs)
	}
	h, err := promise.Hash()
	require.NoError(t, err)
	marker, closer, err := b.db.Get(shardKey(promise.Commitment, h))
	require.NoError(t, err)
	tag, _, err := decodeShardMarkerBackend(marker)
	closer.Close()
	require.NoError(t, err)
	require.Equal(t, packedObjectBackendTag, tag)
	recovered := newPackedBackend(b.object, b.db, 16)
	require.NoError(t, recovered.recover(t.Context()))
	store.shards.packed = recovered
	got, err := store.Get(t.Context(), promise.Commitment)
	require.NoError(t, err)
	require.Equal(t, shard, got)
	count, _, err := store.PruneBefore(t.Context(), time.Unix(3600, 0))
	require.NoError(t, err)
	require.Equal(t, 16, count)
	require.Empty(t, mock.objects)
	recovered.Close()
}
func TestPackedConfigDefaults(t *testing.T) {
	cfg := testObjectStorageConfig()
	cfg.BatchSize = 0
	require.NoError(t, cfg.Validate())
	require.Equal(t, 16, cfg.BatchSize)
	cfg.BatchSize = -1
	require.Error(t, cfg.Validate())
	cfg.BatchSize = 1025
	require.Error(t, cfg.Validate())
}

func TestPackedCanceledUploadRetainsBuffersUntilComplete(t *testing.T) {
	b, _ := newPackedTestBackend(t, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	original := b.object.client.(*s3ObjectClientStub).putObject
	b.object.client.(*s3ObjectClientStub).putObject = func(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
		close(started)
		<-release
		return original(ctx, in, opts...)
	}
	shard := &types.BlobShard{Rows: []*types.BlobRow{{Data: []byte("payload")}}}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- b.Put(ctx, Commitment{}, []byte{1}, shard) }()
	<-started
	cancel()
	select {
	case <-done:
		t.Fatal("returned while S3 still owns caller buffers")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-done)
}
func TestPackedConcurrentDuplicate(t *testing.T) {
	b, mock := newPackedTestBackend(t, 1)
	shard := &types.BlobShard{Rlcs: []byte("data")}
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() { errs <- b.Put(t.Context(), Commitment{}, []byte{1}, shard) }()
	}
	for i := 0; i < 20; i++ {
		require.NoError(t, <-errs)
	}
	require.Equal(t, 1, mock.puts)
}

func TestPackedBucketPinnedIndependently(t *testing.T) {
	clearAWSCredentials(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	b, _ := newPackedTestBackend(t, 16)
	cfg := DefaultStoreConfig()
	cfg.StorageBackend = storageBackendObject
	cfg.ObjectStorage = testObjectStorageConfig()
	cfg.ObjectStorage.BatchSize = 16
	cfg.ObjectStorage.PackedBucket = "packed-bucket"
	b.object.namespace.Endpoint = cfg.ObjectStorage.Endpoint
	storage := &routedStorage{primary: b.object}
	require.NoError(t, storage.openPacked(t.Context(), cfg, b.db))
	defer storage.packed.Close()
	require.Equal(t, "packed-bucket", storage.packed.object.namespace.Bucket)
	require.Equal(t, "bucket", b.object.namespace.Bucket)
	require.NoError(t, b.db.Set(shardKey(Commitment{}, []byte{1}), encodeShardMarkerForBackend(packedObjectBackendTag, 1), pebbledb.Sync))
	cfg.ObjectStorage.PackedBucket = "wrong-bucket"
	require.ErrorIs(t, storage.openPacked(t.Context(), cfg, b.db), ErrStoreIntegrity)
}

func TestPackedPartialCancellationAndShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprint(shutdown), func(t *testing.T) {
			b, mock := newPackedTestBackend(t, 16)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			errs := make(chan error, 15)
			for i := 0; i < 15; i++ {
				i := i
				go func() { errs <- b.Put(ctx, Commitment{}, []byte{1, byte(i)}, &types.BlobShard{Rlcs: []byte("data")}) }()
			}
			require.Eventually(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return len(b.queues[1].requests) == 15 }, time.Second, time.Millisecond)
			time.Sleep(250 * time.Millisecond)
			mock.mu.Lock()
			puts := mock.puts
			mock.mu.Unlock()
			require.Zero(t, puts, "partial group must never flush on a timer")
			if shutdown {
				b.Close()
			} else {
				cancel()
			}
			for i := 0; i < 15; i++ {
				err := <-errs
				if shutdown {
					require.ErrorContains(t, err, "closed before full batch")
				} else {
					require.ErrorIs(t, err, context.Canceled)
				}
			}
			require.Empty(t, mock.objects)
			b.mu.Lock()
			require.Empty(t, b.inflight)
			require.Empty(t, b.queues[1].requests)
			b.mu.Unlock()
		})
	}
}
