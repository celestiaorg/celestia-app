package fibre

import (
	"sync"
	"sync/atomic"
	"time"
)

// peerRegistry maps validator address → per-peer circuit breaker.
// A dead or flaky peer's circuit is opened on failure and subsequent
// blob uploads skip that peer instantly for the cooldown window,
// amortizing the dead-peer cost across many blobs.
type peerRegistry struct {
	failureThreshold int
	cooldown         time.Duration

	mu       sync.Mutex
	breakers map[string]*breaker
}

func newPeerRegistry(failureThreshold int, cooldown time.Duration) *peerRegistry {
	return &peerRegistry{
		failureThreshold: failureThreshold,
		cooldown:         cooldown,
		breakers:         make(map[string]*breaker),
	}
}

// get returns the breaker for addr, lazily creating one on first use.
func (r *peerRegistry) get(addr string) *breaker {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.breakers[addr]
	if !ok {
		b = newBreaker(r.failureThreshold, r.cooldown)
		r.breakers[addr] = b
	}
	return b
}

// breakerState is the state of a circuit breaker.
type breakerState int32

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// String renders a breaker state for logs and traces.
func (s breakerState) String() string {
	switch s {
	case breakerClosed:
		return "closed"
	case breakerOpen:
		return "open"
	case breakerHalfOpen:
		return "half-open"
	}
	return "unknown"
}

// breaker is a per-peer circuit breaker.
//
//   - closed: normal operation; consecutive failures increment a counter.
//     A closed-state Allow hits a lock-free fast path backed by
//     atomicState, so the steady-state all-healthy case never contends
//     on the mutex.
//   - open: failures crossed threshold; Allow rejects until cooldown elapses.
//   - half-open: cooldown elapsed; exactly one probe may proceed.
//     Success → closed. Failure → open (reset timer).
//
// If a half-open probe caller returns without recording an outcome
// (e.g. parent goroutine cancelled before the RPC issued), it must
// call resetHalfOpen so the breaker does not wedge with
// halfOpenInFlight=true forever.
type breaker struct {
	failureThreshold int
	cooldown         time.Duration

	// atomicState mirrors state for the lock-free fast path in Allow.
	// Writers update both under mu; readers may load without the lock.
	atomicState atomic.Int32

	mu               sync.Mutex
	state            breakerState
	consecutiveFail  int
	openedAt         time.Time
	halfOpenInFlight bool
}

func newBreaker(failureThreshold int, cooldown time.Duration) *breaker {
	return &breaker{
		failureThreshold: failureThreshold,
		cooldown:         cooldown,
		// atomicState defaults to 0 == breakerClosed.
	}
}

// Allow reports whether a request should be attempted given the current
// time. The second return value reports whether this call consumed the
// half-open probe slot — if true, the caller MUST end by calling exactly
// one of RecordSuccess, RecordFailure, or resetHalfOpen so the probe
// state does not wedge.
func (b *breaker) Allow(now time.Time) (allowed, halfOpenProbe bool) {
	// Fast path: steady-state closed, no mutex. Covers the overwhelmingly
	// common case where every peer is healthy.
	if breakerState(b.atomicState.Load()) == breakerClosed {
		return true, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerClosed:
		return true, false
	case breakerOpen:
		if now.Sub(b.openedAt) < b.cooldown {
			return false, false
		}
		b.setStateLocked(breakerHalfOpen)
		b.halfOpenInFlight = true
		return true, true
	case breakerHalfOpen:
		if b.halfOpenInFlight {
			return false, false
		}
		b.halfOpenInFlight = true
		return true, true
	}
	return false, false
}

// RecordSuccess closes the circuit and resets failure counters.
// Returns the resulting state and whether it changed, so callers can
// emit a single log line on transitions without racing.
func (b *breaker) RecordSuccess() (newState breakerState, changed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	old := b.state
	b.consecutiveFail = 0
	b.halfOpenInFlight = false
	b.setStateLocked(breakerClosed)
	return b.state, old != b.state
}

// RecordFailure increments the failure counter and opens the circuit if
// the threshold is crossed. A half-open probe failing re-opens the
// circuit immediately. Returns the resulting state and whether it
// changed.
func (b *breaker) RecordFailure(now time.Time) (newState breakerState, changed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	old := b.state
	b.consecutiveFail++
	b.halfOpenInFlight = false
	switch b.state {
	case breakerHalfOpen:
		b.setStateLocked(breakerOpen)
		b.openedAt = now
	case breakerClosed:
		if b.consecutiveFail >= b.failureThreshold {
			b.setStateLocked(breakerOpen)
			b.openedAt = now
		}
	}
	return b.state, old != b.state
}

// resetHalfOpen releases the half-open in-flight flag without changing
// state. Use this when a caller that consumed the half-open probe slot
// exits before recording an outcome (e.g. cancelled before the RPC
// issued). Safe to call unconditionally; no-op outside half-open.
func (b *breaker) resetHalfOpen() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == breakerHalfOpen {
		b.halfOpenInFlight = false
	}
}

// setStateLocked updates both the guarded state and the atomic mirror
// used by the lock-free fast path in Allow. Caller holds b.mu.
func (b *breaker) setStateLocked(s breakerState) {
	b.state = s
	b.atomicState.Store(int32(s))
}
