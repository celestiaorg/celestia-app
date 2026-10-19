package grpc

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	admissionWaiters = 256 // cap on concurrently parked requests
	admissionPerIP   = 8   // admitted+waiting requests per source IP
)

var (
	admissionWait = 3 * time.Second // max wait for budget before rejecting; var for tests
	// requestDeadline bounds total server-side handling of one request, just above
	// the client's 15s RPCTimeout, so a stalled sender can't pin its reservation.
	requestDeadline = 16 * time.Second
)

// AdmissionConfig configures the memory-admission limiter. A non-positive Budget
// or MaxMessage disables admission.
type AdmissionConfig struct {
	Budget     int64 // total bytes that may be reserved at once
	MaxMessage int64 // bytes reserved per request while receiving
	OnReject   func(ctx context.Context, reason string)
}

// admission bounds request receive memory with a FIFO byte budget and a per-IP
// cap. See ADR-031.
type admission struct {
	sem         *semaphore.Weighted
	reservation int64
	onReject    func(context.Context, string)
	perIP       *keyedCounter

	reserved atomic.Int64
	waiting  atomic.Int64
}

func newAdmission(cfg AdmissionConfig) *admission {
	if cfg.Budget <= 0 || cfg.MaxMessage <= 0 {
		return nil
	}
	onReject := cfg.OnReject
	if onReject == nil {
		onReject = func(context.Context, string) {}
	}
	return &admission{
		sem:         semaphore.NewWeighted(cfg.Budget),
		reservation: cfg.MaxMessage,
		onReject:    onReject,
		perIP:       newKeyedCounter(admissionPerIP),
	}
}

// Reserved returns the bytes currently reserved (0 when admission is disabled).
func (a *admission) Reserved() int64 {
	if a == nil {
		return 0
	}
	return a.reserved.Load()
}

// Waiting returns the requests currently parked waiting for budget.
func (a *admission) Waiting() int64 {
	if a == nil {
		return 0
	}
	return a.waiting.Load()
}

// admit reserves one max-size message of budget plus a per-IP slot. A nil
// admission (disabled) returns a nil reservation.
func (a *admission) admit(ctx context.Context, ipKey string) (*reservation, error) {
	if a == nil {
		return nil, nil
	}
	if !a.perIP.tryAcquire(ipKey) {
		return nil, a.reject(ctx, "per_ip")
	}
	if err := a.acquireBudget(ctx); err != nil {
		a.perIP.release(ipKey)
		return nil, err
	}
	return &reservation{adm: a, ipKey: ipKey, held: a.reservation}, nil
}

func (a *admission) acquireBudget(ctx context.Context) error {
	if a.sem.TryAcquire(a.reservation) {
		a.reserved.Add(a.reservation)
		return nil
	}
	// Slow path: count a waiter and fail fast when too many are parked.
	waiting := a.waiting.Add(1)
	defer a.waiting.Add(-1)
	if waiting > admissionWaiters {
		return a.reject(ctx, "memory_overflow")
	}
	acqCtx, cancel := context.WithTimeout(ctx, admissionWait)
	defer cancel()
	if err := a.sem.Acquire(acqCtx, a.reservation); err != nil {
		// Client cancel/deadline is reported as-is; only a live-client timeout is a rejection.
		if cause := context.Cause(ctx); cause != nil {
			return status.Error(cancellationCode(cause), "request cancelled while waiting for memory budget")
		}
		return a.reject(ctx, "memory_timeout")
	}
	a.reserved.Add(a.reservation)
	return nil
}

// reject records a rejection and returns ResourceExhausted with a short RetryInfo hint.
func (a *admission) reject(ctx context.Context, reason string) error {
	a.onReject(ctx, reason)
	st := status.New(grpccodes.ResourceExhausted, "fibre request memory budget exhausted")
	st, _ = st.WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(retryHint())})
	return st.Err()
}

// retryHint is short (~1-2s): budget frees in seconds as requests finish.
func retryHint() time.Duration {
	return time.Second + time.Duration(rand.Int64N(int64(time.Second)))
}

func cancellationCode(cause error) grpccodes.Code {
	if errors.Is(cause, context.DeadlineExceeded) {
		return grpccodes.DeadlineExceeded
	}
	return grpccodes.Canceled
}

// reservation holds budget and a per-IP slot for one request. The per-IP slot is
// released once the body is received (the cap bounds concurrent receives, which is
// where a stalled sender pins memory); the budget is held until the handler ends.
type reservation struct {
	adm   *admission
	ipKey string

	mu       sync.Mutex
	held     int64
	released bool
	recvDone bool
}

// Shrink lowers the reservation to `to`, returning freed budget to the semaphore. Never grows.
func (r *reservation) Shrink(to int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.released || to < 0 || to >= r.held {
		return
	}
	free := r.held - to
	r.held = to
	r.adm.reserved.Add(-free)
	r.adm.sem.Release(free)
}

// releaseRecv frees the per-IP receive slot once the request body is read.
func (r *reservation) releaseRecv() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releasePerIP()
}

func (r *reservation) releasePerIP() {
	if r.recvDone {
		return
	}
	r.recvDone = true
	r.adm.perIP.release(r.ipKey)
}

func (r *reservation) release() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.released {
		return
	}
	r.released = true
	if r.held > 0 {
		r.adm.reserved.Add(-r.held)
		r.adm.sem.Release(r.held)
		r.held = 0
	}
	r.releasePerIP()
}
