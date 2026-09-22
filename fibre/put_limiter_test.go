package fibre

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPutLimiterCapacityAndRelease(t *testing.T) {
	l := NewPutLimiter(2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := l.acquire(ctx, nil, 10)
	require.NoError(t, err)
	second, err := l.acquire(ctx, nil, 20)
	require.NoError(t, err)
	defer second()

	type acquired struct {
		release func()
		err     error
	}
	next := make(chan acquired, 1)
	go func() {
		release, err := l.acquire(ctx, nil, 30)
		next <- acquired{release, err}
	}()
	require.Eventually(t, func() bool { return l.Stats().Waiting == 1 }, time.Second, time.Millisecond)
	stats := l.Stats()
	require.EqualValues(t, 2, stats.Active)
	require.EqualValues(t, 30, stats.ActiveRawBytes)
	require.EqualValues(t, 30, stats.WaitingRawBytes)
	select {
	case <-next:
		t.Fatal("admitted a third blob before storage was released")
	default:
	}

	first()
	first()
	third := <-next
	require.NoError(t, third.err)
	stats = l.Stats()
	require.EqualValues(t, 2, stats.Active)
	require.EqualValues(t, 50, stats.ActiveRawBytes)
	require.Zero(t, stats.Waiting)
	second()
	third.release()
	third.release()
	stats = l.Stats()
	require.Zero(t, stats.Active)
	require.Zero(t, stats.ActiveRawBytes)
	require.Zero(t, stats.WaitingRawBytes)
	require.EqualValues(t, 3, stats.Acquired)
	require.Positive(t, stats.WaitDuration)
}

func TestPutLimiterCancellation(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		name := "context"
		if shutdown {
			name = "client_stop"
		}
		t.Run(name, func(t *testing.T) {
			l := NewPutLimiter(1)
			release, err := l.acquire(context.Background(), nil, 12)
			require.NoError(t, err)
			defer release()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stop := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				got, err := l.acquire(ctx, stop, 24)
				if got != nil {
					got()
				}
				done <- err
			}()
			require.Eventually(t, func() bool { return l.Stats().Waiting == 1 }, time.Second, time.Millisecond)
			want := context.Canceled
			if shutdown {
				close(stop)
				want = ErrClientClosed
			} else {
				cancel()
			}
			require.ErrorIs(t, <-done, want)
			stats := l.Stats()
			require.EqualValues(t, 1, stats.Active)
			require.EqualValues(t, 12, stats.ActiveRawBytes)
			require.Zero(t, stats.Waiting)
			require.Zero(t, stats.WaitingRawBytes)
			require.EqualValues(t, 1, stats.Acquired)
			require.EqualValues(t, 1, stats.Canceled)
		})
	}
}

func TestPutLimiterAlreadyCanceled(t *testing.T) {
	l := NewPutLimiter(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 100 {
		release, err := l.acquire(ctx, nil, 1)
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, release)
	}
	require.Zero(t, l.Stats().Active)
	require.Zero(t, l.Stats().Acquired)
	stop := make(chan struct{})
	close(stop)
	release, err := l.acquire(context.Background(), stop, 1)
	require.ErrorIs(t, err, ErrClientClosed)
	require.Nil(t, release)
	require.Zero(t, l.Stats().Active)
}

func TestPutLimiterConcurrentRelease(t *testing.T) {
	l := NewPutLimiter(3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	errs := make(chan error, 32)
	for range 32 {
		workers.Go(func() {
			for range 100 {
				release, err := l.acquire(ctx, nil, 100)
				if err != nil {
					errs <- err
					return
				}
				if l.Stats().Active > 3 {
					errs <- errors.New("active capacity exceeded")
					release()
					return
				}
				var releases sync.WaitGroup
				for range 2 {
					releases.Go(release)
				}
				releases.Wait()
			}
		})
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	stats := l.Stats()
	require.EqualValues(t, 3200, stats.Acquired)
	require.Zero(t, stats.Active)
	require.Zero(t, stats.ActiveRawBytes)
	require.Zero(t, stats.Waiting)
	require.Zero(t, stats.WaitingRawBytes)
}

func TestPutLimiterDisabledAndInvalid(t *testing.T) {
	var disabled *PutLimiter
	release, err := disabled.acquire(context.Background(), nil, 10)
	require.NoError(t, err)
	release()
	require.Equal(t, PutLimiterStats{}, disabled.Stats())
	_, err = NewPutLimiter(1).acquire(context.Background(), nil, -1)
	require.Error(t, err)
	_, err = (&PutLimiter{}).acquire(context.Background(), nil, 1)
	require.Error(t, err)
	require.Panics(t, func() { NewPutLimiter(0) })
}
