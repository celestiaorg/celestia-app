package fibre

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	ds "github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/stretchr/testify/require"
)

func TestNewWriteBatcherUsesDefaultThresholds(t *testing.T) {
	store := dssync.MutexWrap(ds.NewMapDatastore())
	wb := newWriteBatcher(store)
	t.Cleanup(wb.close)

	require.Equal(t, defaultWriteBatcherMinPending, wb.minPending)
	require.Equal(t, defaultWriteBatcherMaxPending, wb.maxPending)
}

func TestWriteBatcherSubmitReturnsCommitResultAfterEnqueue(t *testing.T) {
	store := newBlockingBatching()
	wb := newWriteBatcherWithOpts(store, writeBatcherOptions{
		queueSize:        1,
		minPending:       1,
		maxPending:       1,
		minBatchBytes:    1,
		targetBatchBytes: 1,
		flushInterval:    time.Hour,
	})

	key := promiseKey([]byte("queued"))
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	put := makeWriteBatcherTestPut(t, "queued", len("value"))
	go func() {
		errCh <- wb.submit(ctx, put)
	}()

	<-store.commitStarted
	cancel()
	close(store.releaseCommit)

	require.NoError(t, <-errCh)

	value, err := store.Get(context.Background(), key)
	require.NoError(t, err)
	require.NotEmpty(t, value)

	wb.close()
}

func TestWriteBatcherCloseWaitsForPendingAndRejectsNewWrites(t *testing.T) {
	store := newBlockingBatching()
	wb := newWriteBatcherWithOpts(store, writeBatcherOptions{
		queueSize:        1,
		minPending:       1,
		maxPending:       1,
		minBatchBytes:    1,
		targetBatchBytes: 1,
		flushInterval:    time.Hour,
	})

	errCh := make(chan error, 1)
	inflightPut := makeWriteBatcherTestPut(t, "inflight", len("value"))
	go func() {
		errCh <- wb.submit(context.Background(), inflightPut)
	}()

	<-store.commitStarted

	closeDone := make(chan struct{})
	go func() {
		wb.close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
		t.Fatal("close returned before the pending commit finished")
	case <-time.After(20 * time.Millisecond):
	}

	require.Eventually(t, func() bool {
		return wb.submitters.isClosed()
	}, time.Second, 10*time.Millisecond)

	require.ErrorIs(t, wb.submit(context.Background(), makeWriteBatcherTestPut(t, "after-close", len("value"))), ErrStoreClosed)

	close(store.releaseCommit)

	require.NoError(t, <-errCh)

	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("close did not return after the pending commit finished")
	}
}

func TestWriteBatcherCoalescesConcurrentSubmits(t *testing.T) {
	const numSubmits = 100

	store := newCountingBatching()
	// Large queue, generous flush delay to encourage coalescing.
	wb := newWriteBatcherWithOpts(store, writeBatcherOptions{
		queueSize:        numSubmits,
		minPending:       16,
		maxPending:       numSubmits,
		minBatchBytes:    1 << 20,
		targetBatchBytes: 4 << 20,
		flushInterval:    1 * time.Second,
	})

	var wg sync.WaitGroup
	wg.Add(numSubmits)
	for i := range numSubmits {
		go func() {
			defer wg.Done()
			err := wb.submit(context.Background(), makeWriteBatcherTestPut(t, fmt.Sprintf("coalesce-%d", i), 1))
			require.NoError(t, err)
		}()
	}
	wg.Wait()
	wb.close()

	commits := int(store.commits.Load())
	t.Logf("coalesced %d submits into %d commits", numSubmits, commits)
	require.Less(t, commits, numSubmits, "expected fewer commits than submits due to coalescing")

	// Verify all entries were written.
	for i := range numSubmits {
		key := promiseKey([]byte(fmt.Sprintf("coalesce-%d", i)))
		_, err := store.Get(context.Background(), key)
		require.NoError(t, err, "entry %d missing", i)
	}
}

func TestWriteBatcherFlushesLargeRequestWithoutWaitingForMinPending(t *testing.T) {
	store := newBlockingBatching()
	wb := newWriteBatcherWithOpts(store, writeBatcherOptions{
		queueSize:        1,
		minPending:       8,
		maxPending:       8,
		minBatchBytes:    1024,
		targetBatchBytes: 1024,
		flushInterval:    time.Hour,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- wb.submit(context.Background(), makeWriteBatcherTestPut(t, "large", 2048))
	}()

	select {
	case <-store.commitStarted:
	case <-time.After(time.Second):
		t.Fatal("large request did not flush immediately")
	}

	close(store.releaseCommit)
	require.NoError(t, <-errCh)
	wb.close()
}

func makeWriteBatcherTestPut(t *testing.T, id string, shardBytes int) *putPayload {
	t.Helper()

	var commitment Commitment
	copy(commitment[:], []byte(id))
	return &putPayload{
		promiseProto: &types.PaymentPromise{
			ChainId:    "test-chain",
			Height:     1,
			Commitment: commitment[:],
			BlobSize:   uint32(shardBytes),
		},
		promiseHash: []byte(id),
		commitment:  commitment,
		shard: &types.BlobShard{
			Rows: []*types.BlobRow{{
				Index: 0,
				Data:  make([]byte, shardBytes),
			}},
			Rlc: &types.BlobShard_Root{Root: make([]byte, 32)},
		},
		pruneAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		ppSize:    (&types.PaymentPromise{ChainId: "test-chain", Height: 1, Commitment: commitment[:], BlobSize: uint32(shardBytes)}).Size(),
		shardSize: (&types.BlobShard{Rows: []*types.BlobRow{{Index: 0, Data: make([]byte, shardBytes)}}, Rlc: &types.BlobShard_Root{Root: make([]byte, 32)}}).Size(),
	}
}

type countingBatching struct {
	ds.Batching
	commits atomic.Int64
}

func newCountingBatching() *countingBatching {
	return &countingBatching{
		Batching: dssync.MutexWrap(ds.NewMapDatastore()),
	}
}

func (c *countingBatching) Batch(ctx context.Context) (ds.Batch, error) {
	batch, err := c.Batching.Batch(ctx)
	if err != nil {
		return nil, err
	}
	return &countingBatch{Batch: batch, parent: c}, nil
}

type countingBatch struct {
	ds.Batch
	parent *countingBatching
}

func (b *countingBatch) Commit(ctx context.Context) error {
	b.parent.commits.Add(1)
	return b.Batch.Commit(ctx)
}

type blockingBatching struct {
	ds.Batching
	commitStarted chan struct{}
	releaseCommit chan struct{}
}

func newBlockingBatching() *blockingBatching {
	return &blockingBatching{
		Batching:      dssync.MutexWrap(ds.NewMapDatastore()),
		commitStarted: make(chan struct{}),
		releaseCommit: make(chan struct{}),
	}
}

func (b *blockingBatching) Batch(ctx context.Context) (ds.Batch, error) {
	batch, err := b.Batching.Batch(ctx)
	if err != nil {
		return nil, err
	}
	return &blockingBatch{
		Batch:  batch,
		parent: b,
	}, nil
}

type blockingBatch struct {
	ds.Batch
	parent *blockingBatching
}

func (b *blockingBatch) Commit(ctx context.Context) error {
	select {
	case <-b.parent.commitStarted:
	default:
		close(b.parent.commitStarted)
	}

	<-b.parent.releaseCommit
	return b.Batch.Commit(ctx)
}
