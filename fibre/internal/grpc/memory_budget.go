package grpc

import (
	"context"
	"math"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/status"
)

// memoryBudget limits total reservations and the portion available to downloads.
type memoryBudget struct {
	mu                                    sync.Mutex
	total, downloadLimit, used, downloads int64
	rejected                              metric.Int64Counter
}

// newMemoryBudget keeps uploadReserve bytes unavailable to downloads; zero total disables enforcement.
func newMemoryBudget(total, uploadReserve int64) *memoryBudget {
	return &memoryBudget{total: total, downloadLimit: total - uploadReserve}
}

// RegisterMetrics exposes total and upload reservations, plus budget rejections by direction.
func (a *memoryBudget) RegisterMetrics(m metric.Meter) error {
	total, err := m.Int64ObservableGauge("fibre.server.rpc.reserved_bytes", metric.WithUnit("By"))
	if err != nil {
		return err
	}
	uploads, err := m.Int64ObservableGauge("fibre.server.rpc.upload_reserved_bytes", metric.WithUnit("By"))
	if err != nil {
		return err
	}
	_, err = m.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		a.mu.Lock()
		used, downloads := a.used, a.downloads
		a.mu.Unlock()
		observer.ObserveInt64(total, used)
		observer.ObserveInt64(uploads, used-downloads)
		return nil
	}, total, uploads)
	if err != nil {
		return err
	}
	a.rejected, err = m.Int64Counter("fibre.server.rpc.memory_rejected")
	return err
}

// memoryLease holds one RPC reservation through the lifetime of its response buffers.
type memoryLease struct {
	budget   *memoryBudget
	download bool
	bytes    int64
	refs     atomic.Int32
}

// reserve acquires the complete estimated working memory before the handler runs.
func (l *memoryLease) reserve(n int64) error {
	a := l.budget
	a.mu.Lock()
	defer a.mu.Unlock()
	if n < 0 || n > math.MaxInt64-l.bytes {
		return status.Error(codes.ResourceExhausted, "invalid RPC memory reservation")
	}
	if a.total > 0 && (n > a.total-a.used || (l.download && n > a.downloadLimit-a.downloads)) {
		if a.rejected != nil {
			a.rejected.Add(context.Background(), 1, metric.WithAttributes(attribute.Bool("download", l.download)))
		}
		return status.Error(codes.ResourceExhausted, "fibre memory budget exhausted; retry later")
	}
	if a.total > 0 {
		a.used += n
		if l.download {
			a.downloads += n
		}
	}
	l.bytes += n
	return nil
}

// release returns the reservation after the handler and response buffers finish.
func (l *memoryLease) release() {
	if l.refs.Add(-1) != 0 {
		return
	}
	a := l.budget
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.total > 0 {
		a.used -= l.bytes
		if l.download {
			a.downloads -= l.bytes
		}
	}
}

// responseCapacity keeps small responses above gRPC's pool threshold so Put releases their lease.
func responseCapacity(size int) int {
	for mem.IsBelowBufferPoolingThreshold(size) {
		size = max(1, size*2)
	}
	return size
}

// Get allocates a response buffer and retains its lease until gRPC calls Put.
func (l *memoryLease) Get(size int) *[]byte {
	buf := make([]byte, size, responseCapacity(size))
	l.refs.Add(1)
	return &buf
}

// Put releases a response buffer and its reference to the lease.
func (l *memoryLease) Put(buf *[]byte) {
	*buf = nil
	l.release()
}
