package grpc

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDownloadPriority(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newMemoryBudget(100)
		upload := newMemoryLease(a, false)
		require.NoError(t, upload.reserve(t.Context(), 100), "uploads can borrow the whole budget")
		download := newMemoryLease(a, true)
		done := make(chan error, 1)
		go func() { done <- download.reserve(t.Context(), 25) }()
		synctest.Wait()
		require.Len(t, a.waiters, 1)
		upload.release()
		require.NoError(t, <-done)
		require.EqualValues(t, 25, a.downloads)
		download.release()
		require.Zero(t, a.used)
	})
}

func TestDownloadPriorityThreshold(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newMemoryBudget(100)
		upload := newMemoryLease(a, false)
		require.NoError(t, upload.reserve(t.Context(), 70))
		download := newMemoryLease(a, true)
		done := make(chan error, 1)
		ctx, cancel := context.WithCancel(t.Context())
		go func() { done <- download.reserve(ctx, 40) }()
		synctest.Wait()
		require.NoError(t, upload.reserve(t.Context(), 5), "uploads can still fill the shared 75%")
		require.Equal(t, codes.ResourceExhausted, status.Code(upload.reserve(t.Context(), 1)))
		cancel()
		require.Equal(t, codes.Canceled, status.Code(<-done))
		require.Empty(t, a.waiters)
		require.NoError(t, upload.reserve(t.Context(), 25), "cancelled demand must stop protecting capacity")
		upload.release()
	})
}

func TestDownloadShareSatisfied(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newMemoryBudget(100)
		download := newMemoryLease(a, true)
		require.NoError(t, download.reserve(t.Context(), 25))
		waiting := newMemoryLease(a, true)
		done := make(chan error, 1)
		ctx, cancel := context.WithCancel(t.Context())
		go func() { done <- waiting.reserve(ctx, 80) }()
		synctest.Wait()
		upload := newMemoryLease(a, false)
		require.NoError(t, upload.reserve(t.Context(), 75), "downloads already hold their protected share")
		cancel()
		require.Equal(t, codes.Canceled, status.Code(<-done))
		upload.release()
		download.release()
	})
}

func TestDownloadQueueCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newMemoryBudget(100)
		upload := newMemoryLease(a, false)
		require.NoError(t, upload.reserve(t.Context(), 90))
		first := newMemoryLease(a, true)
		second := newMemoryLease(a, true)
		firstDone, secondDone := make(chan error, 1), make(chan error, 1)
		ctx, cancel := context.WithCancel(t.Context())
		go func() { firstDone <- first.reserve(ctx, 20) }()
		synctest.Wait()
		go func() { secondDone <- second.reserve(t.Context(), 10) }()
		synctest.Wait()
		require.Len(t, a.waiters, 2, "smaller downloads must not jump the queue")
		cancel()
		require.Equal(t, codes.Canceled, status.Code(<-firstDone))
		require.NoError(t, <-secondDone, "removing the head must wake its successor")
		upload.release()
		second.release()
		require.Zero(t, a.used)
	})
}

func TestDownloadQueueBounds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newMemoryBudget(100)
		upload := newMemoryLease(a, false)
		require.NoError(t, upload.reserve(t.Context(), 100))
		done := make(chan error, downloadQueueLimit)
		for range downloadQueueLimit {
			go func() { done <- newMemoryLease(a, true).reserve(t.Context(), 1) }()
		}
		synctest.Wait()
		require.Len(t, a.waiters, downloadQueueLimit)
		require.Equal(t, codes.ResourceExhausted, status.Code(newMemoryLease(a, true).reserve(t.Context(), 1)))
		time.Sleep(downloadWaitTimeout)
		for range downloadQueueLimit {
			require.Equal(t, codes.ResourceExhausted, status.Code(<-done))
		}
		require.Empty(t, a.waiters)
		require.EqualValues(t, 100, a.used, "waiting must not reserve payload memory")
		upload.release()
	})
}
