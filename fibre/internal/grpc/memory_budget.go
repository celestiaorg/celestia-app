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

// memoryBudget reserves a quarter of shared capacity for uploads.
type memoryBudget struct {
	mu                     sync.Mutex
	total, used, downloads int64
	rejected               metric.Int64Counter
}

// newMemoryBudget creates a shared budget; zero total disables enforcement.
func newMemoryBudget(total int64) *memoryBudget {
	return &memoryBudget{total: total}
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
	conn     *connectionBudget
}

// newMemoryLease starts a lease with the handler's reference already held.
func newMemoryLease(budget *memoryBudget, download bool) *memoryLease {
	l := &memoryLease{budget: budget, download: download}
	l.refs.Store(1)
	return l
}

// reserve acquires the complete estimated working memory before the handler runs.
func (l *memoryLease) reserve(ctx context.Context, n int64) error {
	a := l.budget
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	if n < 0 || n > math.MaxInt64-a.used {
		return status.Error(codes.ResourceExhausted, "invalid RPC memory reservation")
	}
	if a.total > 0 {
		limit := a.total - a.used
		if l.download {
			limit = min(limit, a.total-a.total/4-a.downloads)
		}
		if n > limit {
			return l.reject(ctx)
		}
	}
	a.used += n
	if l.download {
		a.downloads += n
	}
	l.bytes += n
	return nil
}

// reject counts a refused reservation and tells the caller to retry later.
func (l *memoryLease) reject(ctx context.Context) error {
	if l.budget.rejected != nil {
		l.budget.rejected.Add(ctx, 1, metric.WithAttributes(attribute.Bool("download", l.download)))
	}
	return status.Error(codes.ResourceExhausted, "fibre memory budget exhausted; retry later")
}

// release returns the reservation after the handler and response buffers finish.
func (l *memoryLease) release() {
	l.releaseRefs(1)
}

func (l *memoryLease) releaseRefs(n int32) {
	if l.refs.Add(-n) != 0 {
		return
	}
	a := l.budget
	a.mu.Lock()
	defer a.mu.Unlock()
	a.used -= l.bytes
	if l.download {
		a.downloads -= l.bytes
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
	if l.conn != nil {
		l.conn.mu.Lock()
		defer l.conn.mu.Unlock()
		if l.conn.closed && l.conn.handlers == 0 {
			return &buf
		}
		l.conn.leases[l]++
	}
	l.refs.Add(1)
	return &buf
}

// Put releases a response buffer and its reference to the lease.
func (l *memoryLease) Put(buf *[]byte) {
	*buf = nil
	if l.conn != nil {
		l.conn.mu.Lock()
		defer l.conn.mu.Unlock()
		if l.conn.leases[l] == 0 {
			return
		}
		l.conn.leases[l]--
		if l.conn.leases[l] == 0 {
			delete(l.conn.leases, l)
		}
	}
	l.release()
}
