package fibre

import (
	"context"
	"testing"
	"time"

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
	wb := newWriteBatcherWithOpts(store, 1, 1, 1, time.Hour)

	key := ds.NewKey("/queued")
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		errCh <- wb.submit(ctx, []batchEntry{{key: key, value: []byte("value")}})
	}()

	<-store.commitStarted
	cancel()
	close(store.releaseCommit)

	require.NoError(t, <-errCh)

	value, err := store.Get(context.Background(), key)
	require.NoError(t, err)
	require.Equal(t, []byte("value"), value)

	wb.close()
}

func TestWriteBatcherCloseWaitsForPendingAndRejectsNewWrites(t *testing.T) {
	store := newBlockingBatching()
	wb := newWriteBatcherWithOpts(store, 1, 1, 1, time.Hour)

	key := ds.NewKey("/inflight")
	errCh := make(chan error, 1)
	go func() {
		errCh <- wb.submit(context.Background(), []batchEntry{{key: key, value: []byte("value")}})
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

	require.ErrorIs(t, wb.submit(context.Background(), []batchEntry{{key: ds.NewKey("/after-close"), value: []byte("value")}}), ErrStoreClosed)

	close(store.releaseCommit)

	require.NoError(t, <-errCh)

	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("close did not return after the pending commit finished")
	}
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
