package fibre

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestBreakerClosedFastPath verifies the steady-state Allow path does
// not consume half-open probe slots.
func TestBreakerClosedFastPath(t *testing.T) {
	b := newBreaker(3, 100*time.Millisecond)
	now := time.Now()

	for range 1000 {
		allowed, probe := b.Allow(now)
		require.True(t, allowed)
		require.False(t, probe, "closed state must never report a half-open probe")
	}
}

// TestBreakerOpensAfterThreshold verifies that N consecutive failures
// move the breaker to open and subsequent Allow calls reject until
// the cooldown elapses.
func TestBreakerOpensAfterThreshold(t *testing.T) {
	const threshold = 3
	const cooldown = 100 * time.Millisecond
	b := newBreaker(threshold, cooldown)
	now := time.Now()

	// Below threshold: still closed.
	for i := 0; i < threshold-1; i++ {
		state, changed := b.RecordFailure(now)
		require.Equal(t, breakerClosed, state)
		require.False(t, changed, "should not transition below threshold")
		allowed, _ := b.Allow(now)
		require.True(t, allowed)
	}

	// Crossing threshold: transition to open.
	state, changed := b.RecordFailure(now)
	require.Equal(t, breakerOpen, state)
	require.True(t, changed)

	// Still open during cooldown.
	allowed, _ := b.Allow(now)
	require.False(t, allowed)

	// Cooldown elapsed: Allow admits exactly one half-open probe.
	later := now.Add(cooldown + time.Millisecond)
	allowed1, probe1 := b.Allow(later)
	require.True(t, allowed1)
	require.True(t, probe1, "first caller after cooldown must get the half-open probe")

	// Second concurrent caller while probe in flight: rejected.
	allowed2, probe2 := b.Allow(later)
	require.False(t, allowed2)
	require.False(t, probe2)
}

// TestBreakerHalfOpenSuccessCloses verifies a successful half-open
// probe closes the circuit and resets the failure counter.
func TestBreakerHalfOpenSuccessCloses(t *testing.T) {
	b := newBreaker(2, 50*time.Millisecond)
	now := time.Now()

	b.RecordFailure(now)
	b.RecordFailure(now)
	allowed, _ := b.Allow(now)
	require.False(t, allowed, "should be open")

	later := now.Add(100 * time.Millisecond)
	_, probe := b.Allow(later)
	require.True(t, probe)

	state, changed := b.RecordSuccess()
	require.Equal(t, breakerClosed, state)
	require.True(t, changed)

	// After close, next Allow hits the fast path as closed.
	allowed, probe = b.Allow(later)
	require.True(t, allowed)
	require.False(t, probe)
}

// TestBreakerHalfOpenFailureReopens verifies a failed half-open probe
// re-opens the circuit and resets the cooldown clock.
func TestBreakerHalfOpenFailureReopens(t *testing.T) {
	b := newBreaker(2, 50*time.Millisecond)
	t0 := time.Now()

	b.RecordFailure(t0)
	b.RecordFailure(t0)
	// open.

	t1 := t0.Add(100 * time.Millisecond)
	_, probe := b.Allow(t1)
	require.True(t, probe)

	state, changed := b.RecordFailure(t1)
	require.Equal(t, breakerOpen, state)
	require.True(t, changed, "half-open -> open is a state change")

	// Still rejecting just after the re-open.
	allowed, _ := b.Allow(t1)
	require.False(t, allowed)

	// After another full cooldown, probe is admitted again.
	t2 := t1.Add(100 * time.Millisecond)
	_, probe = b.Allow(t2)
	require.True(t, probe)
}

// TestBreakerResetHalfOpen — the regression test for the wedge bug.
// If a goroutine consumes the half-open probe slot and then exits
// (e.g. parent cancelled before the RPC fired) without recording
// success or failure, resetHalfOpen must free the probe slot so the
// breaker does not stay stuck rejecting forever.
func TestBreakerResetHalfOpen(t *testing.T) {
	b := newBreaker(1, 10*time.Millisecond)
	t0 := time.Now()

	b.RecordFailure(t0)
	// open.

	t1 := t0.Add(20 * time.Millisecond)
	_, probe := b.Allow(t1)
	require.True(t, probe, "first caller after cooldown gets probe")

	// Second caller: denied while probe in flight.
	allowed2, _ := b.Allow(t1)
	require.False(t, allowed2)

	// Probe caller exits without recording: the wedge scenario.
	b.resetHalfOpen()

	// Next caller must now get the probe — previously this would
	// have wedged forever with halfOpenInFlight=true.
	_, probe3 := b.Allow(t1)
	require.True(t, probe3, "after resetHalfOpen, probe slot must be available again")
}

// TestBreakerResetHalfOpenNoOpOutsideHalfOpen verifies the reset is
// safe to call unconditionally — it must be a no-op when the breaker
// is closed or open, so callers can defer it without needing to know
// whether they actually consumed the probe slot.
func TestBreakerResetHalfOpenNoOpOutsideHalfOpen(t *testing.T) {
	b := newBreaker(3, 100*time.Millisecond)
	now := time.Now()

	// closed: reset is a no-op.
	b.resetHalfOpen()
	allowed, _ := b.Allow(now)
	require.True(t, allowed)

	// open: reset does not transition state.
	b.RecordFailure(now)
	b.RecordFailure(now)
	b.RecordFailure(now)
	b.resetHalfOpen()
	allowed, _ = b.Allow(now)
	require.False(t, allowed, "open breaker must still reject after resetHalfOpen")
}

// TestBreakerSuccessResetsConsecutiveFailures verifies a success
// between failures resets the "consecutive" counter.
func TestBreakerSuccessResetsConsecutiveFailures(t *testing.T) {
	const threshold = 3
	b := newBreaker(threshold, 100*time.Millisecond)
	now := time.Now()

	b.RecordFailure(now)
	b.RecordFailure(now)
	b.RecordSuccess()
	// consecutiveFail back to 0.
	b.RecordFailure(now)
	b.RecordFailure(now)
	allowed, _ := b.Allow(now)
	require.True(t, allowed, "2 failures after a success should not open the threshold=3 breaker")

	b.RecordFailure(now)
	allowed, _ = b.Allow(now)
	require.False(t, allowed, "3rd consecutive failure should open")
}

// TestPeerRegistryGetIsIdempotent verifies repeated get(addr) returns
// the same breaker (state must not reset on re-access).
func TestPeerRegistryGetIsIdempotent(t *testing.T) {
	r := newPeerRegistry(3, 100*time.Millisecond)

	b1 := r.get("val-a")
	b2 := r.get("val-a")
	require.Same(t, b1, b2)

	b3 := r.get("val-b")
	require.NotSame(t, b1, b3)
}
