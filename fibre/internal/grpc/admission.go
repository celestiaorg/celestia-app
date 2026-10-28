package grpc

import (
	"context"
	"errors"
	"fmt"
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
	admissionPerIP   = 8   // concurrently receiving requests per source IP
)

// Vars so tests can shorten them.
var (
	admissionWait = 3 * time.Second // max wait for budget before rejecting
	// requestDeadline backstops a stalled sender. It is well above the client's
	// 15s RPCTimeout so slow honest max-size uploads are not cut off.
	requestDeadline = 60 * time.Second
)

// AdmissionConfig configures request admission. See ADR-031.
type AdmissionConfig struct {
	Budget     int64 // total bytes that may be reserved at once
	MaxMessage int64 // bytes reserved per request while receiving
	// OnReject is called for every rejection with the method name and reason.
	OnReject func(ctx context.Context, method, reason string)
}

// admission bounds request receive memory with a FIFO byte budget and a per-IP
// cap on concurrently receiving requests.
type admission struct {
	sem         *semaphore.Weighted
	reservation int64
	onReject    func(ctx context.Context, method, reason string)
	perIP       *keyedCounter

	reserved atomic.Int64
	waiting  atomic.Int64
}

func newAdmission(cfg AdmissionConfig) (*admission, error) {
	if cfg.MaxMessage <= 0 || cfg.Budget < cfg.MaxMessage {
		return nil, fmt.Errorf("admission budget %d must fit one max message of %d bytes", cfg.Budget, cfg.MaxMessage)
	}
	onReject := cfg.OnReject
	if onReject == nil {
		onReject = func(context.Context, string, string) {}
	}
	return &admission{
		sem:         semaphore.NewWeighted(cfg.Budget),
		reservation: cfg.MaxMessage,
		onReject:    onReject,
		perIP:       newKeyedCounter(admissionPerIP),
	}, nil
}

// admit reserves one max-size message of budget plus a per-IP receive slot.
func (a *admission) admit(ctx context.Context, method, ipKey string) (*reservation, error) {
	if !a.perIP.tryAcquire(ipKey) {
		return nil, a.reject(ctx, method, "per_ip")
	}
	if err := a.acquire(ctx, method); err != nil {
		a.perIP.release(ipKey)
		return nil, err
	}
	return &reservation{adm: a, ipKey: ipKey, held: a.reservation}, nil
}

func (a *admission) acquire(ctx context.Context, method string) error {
	if !a.sem.TryAcquire(a.reservation) {
		waiting := a.waiting.Add(1)
		defer a.waiting.Add(-1)
		if waiting > admissionWaiters {
			return a.reject(ctx, method, "memory_overflow")
		}
		acqCtx, cancel := context.WithTimeout(ctx, admissionWait)
		defer cancel()
		if err := a.sem.Acquire(acqCtx, a.reservation); err != nil {
			// A client cancel/deadline is reported as-is, not as a rejection.
			if cause := context.Cause(ctx); cause != nil {
				return status.Error(cancellationCode(cause), "request cancelled while waiting for memory budget")
			}
			return a.reject(ctx, method, "memory_timeout")
		}
	}
	a.reserved.Add(a.reservation)
	return nil
}

// reject records a rejection and returns ResourceExhausted with a short RetryInfo
// hint: budget frees within seconds as requests finish.
func (a *admission) reject(ctx context.Context, method, reason string) error {
	a.onReject(ctx, method, reason)
	st := status.New(grpccodes.ResourceExhausted, "fibre request memory budget exhausted")
	st, _ = st.WithDetails(&errdetails.RetryInfo{
		RetryDelay: durationpb.New(time.Second + time.Duration(rand.Int64N(int64(time.Second)))),
	})
	return st.Err()
}

func cancellationCode(cause error) grpccodes.Code {
	if errors.Is(cause, context.DeadlineExceeded) {
		return grpccodes.DeadlineExceeded
	}
	return grpccodes.Canceled
}

// reservation holds budget and a per-IP slot for one request. The per-IP slot is
// freed once the body is read, since stalled senders pin memory while receiving;
// the budget is held until release.
type reservation struct {
	adm   *admission
	ipKey string

	mu       sync.Mutex
	held     int64
	recvDone bool
}

// received frees the per-IP slot and shrinks the reservation to the decoded size.
func (r *reservation) received(size int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releaseIP()
	if size >= 0 && size < r.held {
		r.free(r.held - size)
	}
}

func (r *reservation) release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releaseIP()
	r.free(r.held)
}

func (r *reservation) free(n int64) {
	if n <= 0 {
		return
	}
	r.held -= n
	r.adm.reserved.Add(-n)
	r.adm.sem.Release(n)
}

func (r *reservation) releaseIP() {
	if !r.recvDone {
		r.recvDone = true
		r.adm.perIP.release(r.ipKey)
	}
}
