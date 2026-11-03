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
	gets    int
	deletes int
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
	f.gets++
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
	for _, obj := range in.Delete.Objects {
		f.deletes++
		delete(f.objects, aws.ToString(obj.Key))
	}
	return &s3.DeleteObjectsOutput{}, nil
}

func (f *fakeS3) count() (puts, gets, deletes, objects int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.puts, f.gets, f.deletes, len(f.objects)
}

func parseByteRange(r string) (int64, int64) {
	spec := strings.TrimPrefix(r, "bytes=")
	parts := strings.SplitN(spec, "-", 2)
	start, _ := strconv.ParseInt(parts[0], 10, 64)
	end, _ := strconv.ParseInt(parts[1], 10, 64)
	return start, end
}

// newPackedStore wires an in-memory object store with a packed backend over a
// memory store's Pebble database. enabled mirrors shards_per_object > 1.
func newPackedStore(t *testing.T, shardsPerObject int, enabled bool) (*Store, *fakeS3) {
	t.Helper()
	store := newMarkerTestStore(t)
	fake := newFakeS3()
	object := newObjectBackend(fake, objectNamespace{Bucket: "bucket", Prefix: "fibre", ChainID: "chain", ValidatorAddress: "validator"})
	metrics, err := newServerMetrics(noop.NewMeterProvider().Meter("packed-test"), newOccupancy(0))
	require.NoError(t, err)
	object.metrics = metrics
	store.shards = newRoutedStorage(object, store.shards.primary)
	packed := newPackedBackend(object, store.db, shardsPerObject, 50*time.Millisecond)
	packed.enabled = enabled
	store.shards.packed = packed
	return store, fake
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

func TestPackingConfigDefaultsAndBounds(t *testing.T) {
	cfg := testObjectStorageConfig()
	require.NoError(t, cfg.Validate())
	require.Equal(t, defaultShardsPerObject, cfg.ShardsPerObject)

	cfg = testObjectStorageConfig()
	cfg.ShardsPerObject = maxShardsPerObject + 1
	require.ErrorContains(t, cfg.Validate(), "shards_per_object")

	cfg = testObjectStorageConfig()
	cfg.ShardsPerObject = -1
	require.ErrorContains(t, cfg.Validate(), "shards_per_object")
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
	store := newMarkerTestStore(t)
	err := store.shards.openPacked(t.Context(), DefaultStoreConfig(), store.db, nil)
	require.NoError(t, err)
	require.Nil(t, store.shards.packed)
}

// TestPackedFullBatchOnePutRangeReadAndPrune is the core lifecycle: N shards
// become one PUT, each round-trips via a ranged GET, and the object is deleted
// only once its last member is pruned.
func TestPackedFullBatchOnePutRangeReadAndPrune(t *testing.T) {
	const n = 4
	store, fake := newPackedStore(t, n, true)
	shard := packedTestShard(3)

	promises := make([]*PaymentPromise, n)
	hashes := make([][]byte, n)
	for i := range promises {
		promises[i], hashes[i] = packedTestPromise(t)
	}
	putConcurrently(t, store, promises, shard)

	puts, _, _, objects := fake.count()
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

	_, _, deletes, objects := fake.count()
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
	store, fake := newPackedStore(t, n, true)
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
	_, _, _, objects := fake.count()
	require.Equal(t, 1, objects)

	// Prune the first two members; the object must remain for the third.
	pruned, _, err := store.PruneBefore(t.Context(), time.Unix(190, 0))
	require.NoError(t, err)
	require.Equal(t, 2, pruned)
	_, _, deletes, objects := fake.count()
	require.Zero(t, deletes)
	require.Equal(t, 1, objects)
	got, err := store.Get(t.Context(), promises[2].Commitment)
	require.NoError(t, err)
	require.Equal(t, shard, got)

	// Prune the last member; now the object is deleted.
	pruned, _, err = store.PruneBefore(t.Context(), time.Unix(1000, 0))
	require.NoError(t, err)
	require.Equal(t, 1, pruned)
	_, _, deletes, objects = fake.count()
	require.Equal(t, 1, deletes)
	require.Zero(t, objects)
}

// TestPackedPartialFlushOnTimer verifies a lone shard uploads via the flush
// timer rather than waiting for a full batch.
func TestPackedPartialFlushOnTimer(t *testing.T) {
	store, fake := newPackedStore(t, 8, true) // batch of 8, but only one shard arrives
	shard := packedTestShard(2)
	promise, hash := packedTestPromise(t)

	require.NoError(t, store.Put(t.Context(), promise, shard, promise.CreationTimestamp))
	puts, _, _, objects := fake.count()
	require.Equal(t, 1, puts)
	require.Equal(t, 1, objects)
	has, err := store.Has(t.Context(), promise.Commitment, hash)
	require.NoError(t, err)
	require.True(t, has)
}

// TestPackedFlushOnClose verifies Close uploads a still-partial batch.
func TestPackedFlushOnClose(t *testing.T) {
	store, fake := newPackedStore(t, 8, true)
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

	puts, _, _, objects := fake.count()
	require.Equal(t, 1, puts)
	require.Equal(t, 1, objects)
}

// TestPackedSurvivesRestart simulates a crash/restart: a fresh packed backend
// over the same durable Pebble state serves reads and pruning with no recovery
// step, because every acknowledged shard was committed durably after its PUT.
func TestPackedSurvivesRestart(t *testing.T) {
	const n = 3
	store, fake := newPackedStore(t, n, true)
	shard := packedTestShard(2)
	promises := make([]*PaymentPromise, n)
	for i := range promises {
		promises[i], _ = packedTestPromise(t)
	}
	putConcurrently(t, store, promises, shard)

	// "Restart": drop the in-memory packer and attach a new one over the same db.
	store.shards.packed.Close()
	store.shards.packed = newPackedBackend(store.shards.packed.object, store.db, n, 50*time.Millisecond)
	store.shards.packed.enabled = true

	for _, promise := range promises {
		got, err := store.Get(t.Context(), promise.Commitment)
		require.NoError(t, err)
		require.Equal(t, shard, got)
	}
	pruned, _, err := store.PruneBefore(t.Context(), time.Unix(100, 0))
	require.NoError(t, err)
	require.Equal(t, n, pruned)
	_, _, _, objects := fake.count()
	require.Zero(t, objects)
}

// TestPackedReadableAfterTogglingOff checks that after an operator sets
// shards_per_object back to 1 (enabled false), shards packed earlier are still
// readable and prunable; new writes go back to one object per shard.
func TestPackedReadableAfterTogglingOff(t *testing.T) {
	const n = 2
	store, fake := newPackedStore(t, n, true)
	shard := packedTestShard(2)
	promises := make([]*PaymentPromise, n)
	for i := range promises {
		promises[i], _ = packedTestPromise(t)
	}
	putConcurrently(t, store, promises, shard)
	store.shards.packed.Close()

	// Toggle packing off: the backend stays attached for reads/prune only.
	store.shards.packed = newPackedBackend(store.shards.packed.object, store.db, 1, 50*time.Millisecond)
	store.shards.packed.enabled = false

	got, err := store.Get(t.Context(), promises[0].Commitment)
	require.NoError(t, err)
	require.Equal(t, shard, got)

	// A new write now takes the plain one-object-per-shard path.
	plainPromise, _ := packedTestPromise(t)
	require.NoError(t, store.Put(t.Context(), plainPromise, shard, plainPromise.CreationTimestamp))
	markerData, closer, err := store.db.Get(shardKey(plainPromise.Commitment, mustHash(t, plainPromise)))
	require.NoError(t, err)
	require.Equal(t, byte(objectBackendTag), markerData[1])
	require.NoError(t, closer.Close())

	pruned, _, err := store.PruneBefore(t.Context(), time.Unix(100, 0))
	require.NoError(t, err)
	require.Equal(t, n+1, pruned)
	_, _, _, objects := fake.count()
	require.Zero(t, objects)
}

// TestPackedFailedPutStoresNothing verifies a failed object PUT surfaces the
// error and commits no marker, so occupancy and reads stay clean.
func TestPackedFailedPutStoresNothing(t *testing.T) {
	store, _ := newPackedStore(t, 1, true)
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

// TestPackedRequestReduction measures the S3 PUT/DELETE request reduction from
// packing: storing (and later pruning) the same workload at several
// shards_per_object values. It is the benefit number reported for the PR.
func TestPackedRequestReduction(t *testing.T) {
	const shards = 256
	for _, n := range []int{1, 8, 16, 64} {
		store, fake := newPackedStore(t, n, n > 1)
		shard := packedTestShard(4)
		promises := make([]*PaymentPromise, shards)
		for i := range promises {
			promises[i], _ = packedTestPromise(t)
		}
		putConcurrently(t, store, promises, shard)
		store.shards.packed.Close()
		puts, _, _, objects := fake.count()

		// Prune everything and count DELETE requests.
		_, _, err := store.PruneBefore(t.Context(), time.Unix(1<<40, 0))
		require.NoError(t, err)
		_, _, deletes, remaining := fake.count()
		require.Zero(t, remaining)
		t.Logf("shards_per_object=%-3d  %d shards -> PUTs=%-4d objects=%-4d DELETEs=%-4d  (PUT reduction %.1fx)",
			n, shards, puts, objects, deletes, float64(shards)/float64(puts))
	}
}

// TestPackedEnqueueIdempotent verifies a repeated write of an already-packed
// shard stores nothing new, so it can never be packed twice and strand a member.
func TestPackedEnqueueIdempotent(t *testing.T) {
	store, fake := newPackedStore(t, 1, true)
	shard := packedTestShard(2)
	promise, hash := packedTestPromise(t)

	require.NoError(t, store.Put(t.Context(), promise, shard, promise.CreationTimestamp))
	require.NoError(t, store.Put(t.Context(), promise, shard, promise.CreationTimestamp))
	puts, _, _, objects := fake.count()
	require.Equal(t, 1, puts)
	require.Equal(t, 1, objects)

	has, err := store.Has(t.Context(), promise.Commitment, hash)
	require.NoError(t, err)
	require.True(t, has)
}

// TestPackedConcurrentClose verifies that a second Close does not return (and so
// cannot let the database close) while the first Close's upload is in flight.
func TestPackedConcurrentClose(t *testing.T) {
	store, _ := newPackedStore(t, 1, true)
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

// TestPackedConcurrentDuplicate submits the same shard twice while the first
// upload is blocked mid-PUT. The duplicate must join the in-flight request, not
// start a second pack that would strand a member.
func TestPackedConcurrentDuplicate(t *testing.T) {
	store, _ := newPackedStore(t, 1, true) // batch of 1: the first Put flushes and blocks in PUT
	fake := newFakeS3()
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	store.shards.packed.object.client = &fakeBlockingS3{fakeS3: fake, release: release, entered: entered}
	promise, hash := packedTestPromise(t)

	errs := make(chan error, 2)
	go func() { errs <- store.Put(t.Context(), promise, packedTestShard(2), promise.CreationTimestamp) }()
	<-entered // the first upload is blocked inside PutObject, registered in inflight
	go func() { errs <- store.Put(t.Context(), promise, packedTestShard(2), promise.CreationTimestamp) }()

	// Let the second enqueue observe the in-flight request before releasing.
	require.Eventually(t, func() bool {
		store.shards.packed.mu.Lock()
		defer store.shards.packed.mu.Unlock()
		return store.shards.packed.inflight[string(shardKey(promise.Commitment, hash))] != nil
	}, time.Second, time.Millisecond)
	close(release)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)

	puts, _, _, objects := fake.count()
	require.Equal(t, 1, puts, "the duplicate must not start a second pack")
	require.Equal(t, 1, objects)
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

func mustHash(t *testing.T, promise *PaymentPromise) []byte {
	t.Helper()
	hash, err := promise.Hash()
	require.NoError(t, err)
	return hash
}
