package fibre

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

// fakeS3 is an in-memory object store that honours range GETs so packed shard
// round-trips can be exercised without a live S3.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    int
	deletes int // DELETE requests, not keys
}

func newFakeS3() *fakeS3 { return &fakeS3{objects: map[string][]byte{}} }

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	body, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.puts++
	f.objects[aws.ToString(in.Key)] = body
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.objects[aws.ToString(in.Key)]
	if !ok {
		return nil, &smithy.GenericAPIError{Code: "NoSuchKey"}
	}
	if r := aws.ToString(in.Range); r != "" {
		start, end := parseByteRange(r)
		data = data[start : end+1]
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(data))}, nil
}

func (f *fakeS3) HeadObject(_ context.Context, in *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.objects[aws.ToString(in.Key)]; !ok {
		return nil, &smithy.GenericAPIError{Code: "NotFound"}
	}
	return &s3.HeadObjectOutput{}, nil
}

func (f *fakeS3) DeleteObject(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes++
	delete(f.objects, aws.ToString(in.Key))
	return &s3.DeleteObjectOutput{}, nil
}

func (f *fakeS3) DeleteObjects(_ context.Context, in *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes++
	for _, obj := range in.Delete.Objects {
		delete(f.objects, aws.ToString(obj.Key))
	}
	return &s3.DeleteObjectsOutput{}, nil
}

func (f *fakeS3) count() (puts, deletes, objects int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.puts, f.deletes, len(f.objects)
}

func parseByteRange(r string) (int64, int64) {
	spec := strings.TrimPrefix(r, "bytes=")
	parts := strings.SplitN(spec, "-", 2)
	start, _ := strconv.ParseInt(parts[0], 10, 64)
	end, _ := strconv.ParseInt(parts[1], 10, 64)
	return start, end
}

// newPackedStore wires an in-memory object store with a packed backend over a
// memory store's Pebble database.
func newPackedStore(t *testing.T, shardsPerObject int) (*Store, *fakeS3) {
	t.Helper()
	store := newMarkerTestStore(t)
	fake := newFakeS3()
	object := newObjectBackend(fake, objectNamespace{Bucket: "bucket", Prefix: "fibre", ChainID: "chain", ValidatorAddress: "validator"})
	metrics, err := newServerMetrics(noop.NewMeterProvider().Meter("packed-test"), newOccupancy(0))
	require.NoError(t, err)
	object.metrics = metrics
	store.shards = newRoutedStorage(object, store.shards.primary)
	store.shards.packed = newTestPackedBackend(object, store.db, shardsPerObject)
	return store, fake
}

// newTestPackedBackend shortens the flush delay so partial batches upload fast.
func newTestPackedBackend(object *objectBackend, db *pebbledb.DB, shardsPerObject int) *packedBackend {
	packed := newPackedBackend(object, db, shardsPerObject)
	packed.flushDelay = 50 * time.Millisecond
	return packed
}

func packedTestPromise(t *testing.T) (*PaymentPromise, []byte) {
	t.Helper()
	promise := &PaymentPromise{
		ChainID:           "test-chain",
		SignerKey:         secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey),
		Commitment:        generateCommitment(),
		CreationTimestamp: time.Unix(1, 0),
		Signature:         []byte{1},
	}
	hash, err := promise.Hash()
	require.NoError(t, err)
	return promise, hash
}

func packedTestShard(n int) *types.BlobShard {
	rows := make([]*types.BlobRow, n)
	for i := range rows {
		rows[i] = &types.BlobRow{Index: uint32(i), Data: bytes.Repeat([]byte{byte(i + 1)}, 64)}
	}
	return &types.BlobShard{Rlcs: []byte("rlc"), Rows: rows}
}

// putConcurrently stores every (promise, shard) pair concurrently so a batch of
// shardsPerObject fills from count-based flush, not just the timer.
func putConcurrently(t *testing.T, store *Store, promises []*PaymentPromise, shard *types.BlobShard) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make([]error, len(promises))
	for i, promise := range promises {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = store.Put(t.Context(), promise, shard, promise.CreationTimestamp)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
}

func TestPackedMarkerRoundTrip(t *testing.T) {
	var packID [packIDSize]byte
	copy(packID[:], bytes.Repeat([]byte{0xab}, packIDSize))
	marker := encodePackedShardMarker(4096, 1<<20, packID)

	tag, size, err := decodeShardMarkerBackend(marker)
	require.NoError(t, err)
	require.Equal(t, packedObjectBackendTag, tag)
	require.Equal(t, int64(4096), size)

	gotID, offset, gotSize, err := decodePackedShardMarker(marker)
	require.NoError(t, err)
	require.Equal(t, packID, gotID)
	require.Equal(t, int64(1<<20), offset)
	require.Equal(t, int64(4096), gotSize)

	// A plain object marker is not decodable as packed.
	_, _, _, err = decodePackedShardMarker(encodeShardMarkerForBackend(objectBackendTag, 10))
	require.ErrorIs(t, err, ErrStoreIntegrity)
}

// TestPackingDisabledNoBackend proves that with shards_per_object = 1 no packed
// backend is attached and the plain one-object-per-shard path is unchanged.
func TestPackingDisabledNoBackend(t *testing.T) {
	require.Nil(t, newMarkerTestStore(t).shards.packed)
}

// TestPackedFullBatchOnePutRangeReadAndPrune is the core lifecycle: N shards
// become one PUT, each round-trips via a ranged GET, and the object is deleted
// only once its last member is pruned.
func TestPackedFullBatchOnePutRangeReadAndPrune(t *testing.T) {
	const n = 4
	store, fake := newPackedStore(t, n)
	shard := packedTestShard(3)

	promises := make([]*PaymentPromise, n)
	hashes := make([][]byte, n)
	for i := range promises {
		promises[i], hashes[i] = packedTestPromise(t)
	}
	putConcurrently(t, store, promises, shard)

	puts, _, objects := fake.count()
	require.Equal(t, 1, puts, "the whole batch is a single PUT")
	require.Equal(t, 1, objects)

	// Every shard reads back through a ranged GET of the shared object.
	for i, promise := range promises {
		got, err := store.Get(t.Context(), promise.Commitment)
		require.NoError(t, err)
		require.Equal(t, shard, got)
		has, err := store.Has(t.Context(), promise.Commitment, hashes[i])
		require.NoError(t, err)
		require.True(t, has)
	}

	size, err := store.Size(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(n)*shardBinarySize(shard), size)

	// Prune everything before a far-future time: all members expire together.
	pruned, freed, err := store.PruneBefore(t.Context(), time.Unix(100, 0))
	require.NoError(t, err)
	require.Equal(t, n, pruned)
	require.Equal(t, int64(n)*shardBinarySize(shard), freed)

	_, deletes, objects := fake.count()
	require.Equal(t, 1, deletes, "the object is deleted exactly once")
	require.Zero(t, objects)
	size, err = store.Size(t.Context())
	require.NoError(t, err)
	require.Zero(t, size)
}

// TestPackedObjectRetainedUntilLastShardExpires checks delete granularity: a
// packed object survives while any member is still live.
func TestPackedObjectRetainedUntilLastShardExpires(t *testing.T) {
	const n = 3
	store, fake := newPackedStore(t, n)
	shard := packedTestShard(2)

	promises := make([]*PaymentPromise, n)
	for i := range promises {
		promises[i], _ = packedTestPromise(t)
	}
	// Stagger prune times by whole minutes (the prune index is minute-precision)
	// so members expire in separate passes.
	pruneAts := []time.Time{time.Unix(60, 0), time.Unix(120, 0), time.Unix(180, 0)}
	var wg sync.WaitGroup
	for i, promise := range promises {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, store.Put(t.Context(), promise, shard, pruneAts[i]))
		}()
	}
	wg.Wait()
	_, _, objects := fake.count()
	require.Equal(t, 1, objects)

	// Prune the first two members; the object must remain for the third.
	pruned, _, err := store.PruneBefore(t.Context(), time.Unix(190, 0))
	require.NoError(t, err)
	require.Equal(t, 2, pruned)
	_, deletes, objects := fake.count()
	require.Zero(t, deletes)
	require.Equal(t, 1, objects)
	got, err := store.Get(t.Context(), promises[2].Commitment)
	require.NoError(t, err)
	require.Equal(t, shard, got)

	// Prune the last member; now the object is deleted.
	pruned, _, err = store.PruneBefore(t.Context(), time.Unix(1000, 0))
	require.NoError(t, err)
	require.Equal(t, 1, pruned)
	_, deletes, objects = fake.count()
	require.Equal(t, 1, deletes)
	require.Zero(t, objects)
}

// TestPackedPartialFlushOnTimer verifies a lone shard uploads via the flush
// timer rather than waiting for a full batch.
func TestPackedPartialFlushOnTimer(t *testing.T) {
	store, fake := newPackedStore(t, 8) // batch of 8, but only one shard arrives
	shard := packedTestShard(2)
	promise, hash := packedTestPromise(t)

	require.NoError(t, store.Put(t.Context(), promise, shard, promise.CreationTimestamp))
	puts, _, objects := fake.count()
	require.Equal(t, 1, puts)
	require.Equal(t, 1, objects)
	has, err := store.Has(t.Context(), promise.Commitment, hash)
	require.NoError(t, err)
	require.True(t, has)
}

// TestPackedFlushOnClose verifies Close uploads a still-partial batch.
func TestPackedFlushOnClose(t *testing.T) {
	store, fake := newPackedStore(t, 8)
	store.shards.packed.flushDelay = time.Hour // keep the timer from firing
	shard := packedTestShard(2)
	promise, _ := packedTestPromise(t)

	done := make(chan error, 1)
	go func() { done <- store.Put(t.Context(), promise, shard, promise.CreationTimestamp) }()

	// Give the Put time to enqueue, then close: Close flushes the partial batch.
	packed := store.shards.packed
	require.Eventually(t, func() bool {
		packed.mu.Lock()
		defer packed.mu.Unlock()
		return len(packed.queue) == 1
	}, time.Second, time.Millisecond)
	packed.Close()
	require.NoError(t, <-done)

	puts, _, objects := fake.count()
	require.Equal(t, 1, puts)
	require.Equal(t, 1, objects)
}

// TestPackedSurvivesRestart simulates a restart: a fresh packed backend over
// the same durable Pebble state serves reads and pruning with no recovery step,
// because every acknowledged shard was committed durably after its PUT. That
// holds too when the operator set shards_per_object back to 1 in between; new
// writes then return to one object per shard.
func TestPackedSurvivesRestart(t *testing.T) {
	const n = 3
	tests := []struct {
		name            string
		shardsPerObject int
		wantTag         shardBackendTag
	}{
		{"same setting", n, packedObjectBackendTag},
		{"packing turned off", 1, objectBackendTag},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, fake := newPackedStore(t, n)
			shard := packedTestShard(2)
			promises := make([]*PaymentPromise, n)
			for i := range promises {
				promises[i], _ = packedTestPromise(t)
			}
			putConcurrently(t, store, promises, shard)

			// "Restart": drop the in-memory packer and attach a new one over the same db.
			store.shards.packed.Close()
			store.shards.packed = newTestPackedBackend(store.shards.packed.object, store.db, tt.shardsPerObject)

			for _, promise := range promises {
				got, err := store.Get(t.Context(), promise.Commitment)
				require.NoError(t, err)
				require.Equal(t, shard, got)
			}

			promise, hash := packedTestPromise(t)
			require.NoError(t, store.Put(t.Context(), promise, shard, promise.CreationTimestamp))
			markerData, closer, err := store.db.Get(shardKey(promise.Commitment, hash))
			require.NoError(t, err)
			require.Equal(t, byte(tt.wantTag), markerData[1])
			require.NoError(t, closer.Close())

			pruned, _, err := store.PruneBefore(t.Context(), time.Unix(100, 0))
			require.NoError(t, err)
			require.Equal(t, n+1, pruned)
			_, _, objects := fake.count()
			require.Zero(t, objects)
		})
	}
}

// TestPackedFailedPutStoresNothing verifies a failed object PUT surfaces the
// error and commits no marker, so occupancy and reads stay clean.
func TestPackedFailedPutStoresNothing(t *testing.T) {
	store, _ := newPackedStore(t, 2)
	store.shards.packed.object.client = &s3ObjectClientStub{
		putObject: func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			return nil, fmt.Errorf("put failed")
		},
	}
	promise, hash := packedTestPromise(t)
	err := store.Put(t.Context(), promise, packedTestShard(2), promise.CreationTimestamp)
	require.ErrorContains(t, err, "put failed")

	has, accounted, err := store.shardStatus(t.Context(), promise.Commitment, hash)
	require.NoError(t, err)
	require.False(t, has)
	require.False(t, accounted)
}

// TestPackedEnqueueIdempotent verifies a repeated write of an already-packed
// shard stores nothing new, so it can never be packed twice and strand a member.
func TestPackedEnqueueIdempotent(t *testing.T) {
	store, fake := newPackedStore(t, 2)
	shard := packedTestShard(2)
	promise, hash := packedTestPromise(t)

	require.NoError(t, store.Put(t.Context(), promise, shard, promise.CreationTimestamp))
	require.NoError(t, store.Put(t.Context(), promise, shard, promise.CreationTimestamp))
	puts, _, objects := fake.count()
	require.Equal(t, 1, puts)
	require.Equal(t, 1, objects)

	has, err := store.Has(t.Context(), promise.Commitment, hash)
	require.NoError(t, err)
	require.True(t, has)
}

// TestPackedConcurrentClose verifies that a second Close does not return (and so
// cannot let the database close) while the first Close's upload is in flight.
func TestPackedConcurrentClose(t *testing.T) {
	store, _ := newPackedStore(t, 2)
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	store.shards.packed.object.client = &fakeBlockingS3{fakeS3: newFakeS3(), release: release, entered: entered}
	promise, _ := packedTestPromise(t)

	go func() { _ = store.Put(t.Context(), promise, packedTestShard(2), promise.CreationTimestamp) }()
	<-entered // the upload is now blocked inside PutObject

	var wg sync.WaitGroup
	returned := make(chan struct{}, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); store.shards.packed.Close(); returned <- struct{}{} }()
	}
	select {
	case <-returned:
		t.Fatal("Close returned while an upload was still in flight")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	wg.Wait()
}

type fakeBlockingS3 struct {
	*fakeS3
	release <-chan struct{}
	entered chan struct{}
}

func (f *fakeBlockingS3) PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	select {
	case f.entered <- struct{}{}:
	default:
	}
	<-f.release
	return f.fakeS3.PutObject(ctx, in, opts...)
}
