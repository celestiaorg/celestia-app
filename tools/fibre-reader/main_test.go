package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

func TestAssignedReads(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prefix uint64
		reads  int
		want   []int
	}{
		{"original owner", 2, 1, []int{0, 0, 1}},
		{"overlap wraps", 2, 2, []int{1, 0, 1}},
		{"all readers", 2, 3, []int{1, 1, 1}},
		{"more reads than readers", 2, 5, []int{2, 1, 2}},
		{"multiple full rounds", 2, 6, []int{2, 2, 2}},
		{"single reader", 12, 5, []int{5}},
		{"maximum reads", math.MaxUint64, math.MaxInt, []int{math.MaxInt}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var commitment fibre.Commitment
			binary.BigEndian.PutUint64(commitment[:8], tc.prefix)
			for index, want := range tc.want {
				require.Equal(t, want, assignedReads(commitment, len(tc.want), index, tc.reads), "reader %d", index)
			}
		})
	}
}

func TestAssignedReadsTotalBalanceAndCompatibility(t *testing.T) {
	for _, count := range []int{1, 2, 3, 10, 30, 97} {
		for _, reads := range []int{1, 2, 3, 10, 30, 100, math.MaxInt} {
			for _, prefix := range []uint64{0, 1, 2, 29, math.MaxInt, math.MaxUint64} {
				var commitment fibre.Commitment
				binary.BigEndian.PutUint64(commitment[:8], prefix)
				total, least, most := 0, math.MaxInt, 0
				for index := range count {
					got := assignedReads(commitment, count, index, reads)
					require.GreaterOrEqual(t, got, 0)
					total += got
					least, most = min(least, got), max(most, got)
					if reads == 1 {
						require.Equal(t, prefix%uint64(count) == uint64(index), got == 1)
					}
				}
				require.Equal(t, reads, total, "count=%d prefix=%d", count, prefix)
				require.LessOrEqual(t, most-least, 1)
			}
		}
	}
}

func TestAssignedReadsLargeReaderIndices(t *testing.T) {
	var commitment fibre.Commitment
	binary.BigEndian.PutUint64(commitment[:8], uint64(math.MaxInt-1))
	require.Equal(t, 1, assignedReads(commitment, math.MaxInt, math.MaxInt-1, 2))
	require.Equal(t, 1, assignedReads(commitment, math.MaxInt, 0, 2))
	require.Equal(t, 0, assignedReads(commitment, math.MaxInt, 1, 2))
}

func TestRunRejectsInvalidReadsPerBlob(t *testing.T) {
	for _, reads := range []int{0, -1, math.MinInt} {
		t.Run(fmt.Sprint(reads), func(t *testing.T) {
			err := run(config{readerCount: 1, readerIndex: 0, readsPerBlob: reads, downloadConcurrency: 1})
			require.EqualError(t, err, fmt.Sprintf("--reads-per-blob must be >= 1, got %d", reads))
		})
	}
}

func TestHandlePayForFibreCountsBlobsAndAssignedReads(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Count assignments without starting network downloads.
	var commitment fibre.Commitment
	binary.BigEndian.PutUint64(commitment[:8], 2)
	msg := &fibretypes.MsgPayForFibre{PaymentPromise: fibretypes.PaymentPromise{Commitment: commitment[:]}}
	for _, tc := range []struct {
		name    string
		reads   int
		index   int
		owned   int64
		planned int64
	}{
		{"repeated", 5, 0, 1, 2},
		{"one of repeated", 5, 1, 1, 1},
		{"skipped", 2, 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &stats{}
			var wg sync.WaitGroup
			cfg := config{readerCount: 3, readerIndex: tc.index, readsPerBlob: tc.reads}
			handlePayForFibre(ctx, msg, &cmttypes.Block{}, cfg, nil, &wg, nil, st, nil, nil)
			wg.Wait()
			require.Equal(t, int64(1), st.blobsSeen.Load())
			require.Equal(t, tc.owned, st.blobsOwned.Load())
			require.Equal(t, 1-tc.owned, st.blobsSkipped.Load())
			require.Equal(t, tc.planned, st.downloadsAssigned.Load())
			require.Zero(t, st.downloadsSuccess.Load())
			require.Zero(t, st.downloadsFailed.Load())
		})
	}
}

func TestScheduleDownloadsRepeatsAndResetsQueueTime(t *testing.T) {
	sem := make(chan struct{}, 1)
	var wg sync.WaitGroup
	var numbers []int
	var queued []time.Time
	var finished []time.Time
	scheduleDownloads(t.Context(), 5, sem, &wg, func(queuedAt time.Time, number int) {
		numbers = append(numbers, number)
		queued = append(queued, queuedAt)
		finished = append(finished, time.Now())
	})
	wg.Wait()
	require.Equal(t, []int{1, 2, 3, 4, 5}, numbers)
	for i := 1; i < len(queued); i++ {
		require.False(t, queued[i].Before(finished[i-1]))
	}
	require.Empty(t, sem)
}

func TestScheduleDownloadsCancellation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		cancelBefore  bool
		fillSemaphore bool
	}{
		{"before scheduling", true, false},
		{"while waiting", false, true},
		{"between repeats", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				sem := make(chan struct{}, 1)
				if tc.fillSemaphore {
					sem <- struct{}{}
				}
				if tc.cancelBefore {
					cancel()
				}
				var wg sync.WaitGroup
				calls := 0
				scheduleDownloads(ctx, math.MaxInt, sem, &wg, func(time.Time, int) {
					calls++
					cancel()
				})
				if tc.fillSemaphore {
					synctest.Wait() // The download goroutine must be blocked on the full semaphore.
					cancel()
				}
				done := make(chan struct{})
				go func() { wg.Wait(); close(done) }()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("scheduled downloads did not stop after cancellation")
				}
				wantCalls := 1
				if tc.fillSemaphore || tc.cancelBefore {
					wantCalls = 0
				}
				require.Equal(t, wantCalls, calls)
				if tc.fillSemaphore {
					<-sem
				}
				require.Empty(t, sem)
			})
		})
	}
}

func TestScheduleDownloadsShareConcurrencyLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sem := make(chan struct{}, 2)
	entered := make(chan struct{}, 15)
	release := make(chan struct{})
	var active, maximum, calls atomic.Int64
	var wg sync.WaitGroup
	for range 3 {
		scheduleDownloads(ctx, 5, sem, &wg, func(time.Time, int) {
			n := active.Add(1)
			for old := maximum.Load(); n > old; old = maximum.Load() {
				if maximum.CompareAndSwap(old, n) {
					break
				}
			}
			entered <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
			calls.Add(1)
			active.Add(-1)
		})
	}
	for range cap(sem) {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("downloads did not fill available concurrency")
		}
	}
	close(release)
	wg.Wait()
	require.Equal(t, int64(15), calls.Load())
	require.Equal(t, int64(2), maximum.Load())
	require.Zero(t, active.Load())
	require.Empty(t, sem)
}
