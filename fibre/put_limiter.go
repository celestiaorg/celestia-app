package fibre

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"
)

// PutMemoryBytes estimates the memory one in-flight Put of dataLen bytes
// holds outside the caller's payload: parity rows plus ~10% for trees,
// partial rows and upload framing.
func (c BlobConfig) PutMemoryBytes(dataLen int) int64 {
	return int64(c.RowSize(dataLen)) * int64(c.ParityRows) * 11 / 10
}

// DefaultPutLimit returns how many Puts of up to maxDataLen bytes fit in
// memBytes, capped at GOMAXPROCS since more concurrent encodes don't add
// throughput.
func DefaultPutLimit(memBytes int64, maxDataLen int) int {
	n := int(memBytes / DefaultBlobConfigV0().PutMemoryBytes(maxDataLen))
	return max(1, min(n, runtime.GOMAXPROCS(0)))
}

// PutLimiter bounds concurrent Put encodings and upload payloads, including
// background uploads after quorum. It does not bound cached pool allocations.
type PutLimiter struct {
	slots chan struct{}
	mu    sync.Mutex
	stats PutLimiterStats
}

// PutLimiterStats reports admission and original payload sizes. Raw byte
// counts exclude parity, scratch, transport buffers and cached allocations.
type PutLimiterStats struct {
	Limit, Active, Waiting          int64
	ActiveRawBytes, WaitingRawBytes int64
	Acquired, Canceled              int64
	WaitDuration                    time.Duration
}

// NewPutLimiter creates a limit shared by one or more clients. maxActive must
// be positive; choose it using the maximum blob size and codec working set.
func NewPutLimiter(maxActive int) *PutLimiter {
	if maxActive <= 0 {
		panic("fibre: Put limit must be positive")
	}
	return &PutLimiter{
		slots: make(chan struct{}, maxActive),
		stats: PutLimiterStats{Limit: int64(maxActive)},
	}
}

// Stats returns a consistent snapshot. A nil limiter reports zero values.
func (l *PutLimiter) Stats() PutLimiterStats {
	if l == nil {
		return PutLimiterStats{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stats
}

func (l *PutLimiter) acquire(ctx context.Context, stop <-chan struct{}, rawBytes int64) (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	if rawBytes < 0 {
		return nil, fmt.Errorf("fibre: negative Put payload size")
	}
	if l.slots == nil {
		return nil, fmt.Errorf("fibre: PutLimiter must be created with NewPutLimiter")
	}
	start := time.Now()
	l.mu.Lock()
	l.stats.Waiting++
	l.stats.WaitingRawBytes += rawBytes
	l.mu.Unlock()

	var err error
	select {
	case l.slots <- struct{}{}:
		// Cancellation may race a newly available slot. Do not start encoding
		// when cancellation or client shutdown is already observable.
		err = ctx.Err()
		select {
		case <-stop:
			err = ErrClientClosed
		default:
		}
		if err != nil {
			<-l.slots
		}
	case <-ctx.Done():
		err = ctx.Err()
	case <-stop:
		err = ErrClientClosed
	}

	l.mu.Lock()
	l.stats.Waiting--
	l.stats.WaitingRawBytes -= rawBytes
	l.stats.WaitDuration += time.Since(start)
	if err != nil {
		l.stats.Canceled++
	} else {
		l.stats.Active++
		l.stats.ActiveRawBytes += rawBytes
		l.stats.Acquired++
	}
	l.mu.Unlock()
	if err != nil {
		return nil, err
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.stats.Active--
			l.stats.ActiveRawBytes -= rawBytes
			<-l.slots
			l.mu.Unlock()
		})
	}, nil
}
