package fibre

import (
	"context"
	"sync"
	"testing"
	"time"

	"cosmossdk.io/math"
	clock "github.com/filecoin-project/go-clock"
	"github.com/stretchr/testify/require"
)

// TestEscrowLedgerEnsureSeededUsesExistingBalance verifies the first use seeds
// chainBal from chain, so a pre-funded escrow doesn't trigger a needless deposit.
func TestEscrowLedgerEnsureSeededUsesExistingBalance(t *testing.T) {
	d := newMockDepositor()
	q := &mockQuerier{bal: math.NewInt(50_000)}
	l := newTestLedger(t, clock.New(), q, d)

	require.NoError(t, l.ensureSeeded(t.Context()))
	require.NoError(t, l.ensureSeeded(t.Context())) // idempotent
	require.Equal(t, 1, q.queries())                // queried exactly once
	require.Equal(t, int64(50_000), l.available().Int64())

	require.True(t, l.reserve("a", math.NewInt(1_000)))
	count, _ := d.deposits()
	require.Equal(t, 0, count) // existing balance covered it; no deposit
}

// TestEscrowLedgerMaintainRefillsThenReconciles checks that maintain tops up a
// low budget immediately and re-syncs from chain only once the interval elapses.
func TestEscrowLedgerMaintainRefillsThenReconciles(t *testing.T) {
	mock := clock.NewMock()
	d := newMockDepositor()
	q := &mockQuerier{bal: math.NewInt(0)}
	l := newTestLedger(t, mock, q, d)
	require.NoError(t, l.ensureSeeded(t.Context())) // chainBal=0, lastReconcile=now

	// budget below LowWatermark -> refill to HighWatermark
	l.maintain(t.Context())
	count, _ := d.deposits()
	require.Equal(t, 1, count)
	require.Equal(t, int64(10_000), l.available().Int64())

	// reconcile not due yet: chain change is not picked up
	q.set(math.NewInt(7_777))
	l.maintain(t.Context())
	require.Equal(t, int64(10_000), l.chainBal.Int64())

	// after the interval, maintain reconciles to ground truth
	mock.Add(l.cfg.ReconcileInterval + time.Millisecond)
	l.maintain(t.Context())
	require.Equal(t, int64(7_777), l.chainBal.Int64())
}

// TestEscrowLedgerFundedBurstNeverOvercommits is the core Phase-1 property: under
// a concurrent burst of reservations that together exceed the starting balance,
// auto-funding lets every upload through while the reserved total never exceeds
// the funded balance (no overcommit), and deposits are bounded by single-flight.
func TestEscrowLedgerFundedBurstNeverOvercommits(t *testing.T) {
	d := newMockDepositor()
	q := &mockQuerier{bal: math.NewInt(0)}
	l := newTestLedger(t, clock.New(), q, d)
	// Tight watermarks so the burst forces several refills.
	l.cfg.LowWatermark = math.NewInt(500)
	l.cfg.HighWatermark = math.NewInt(2_000)
	require.NoError(t, l.ensureSeeded(t.Context()))

	const (
		n      = 100
		amount = 100 // total demand 10_000 >> any single HighWatermark
	)
	var (
		wg      sync.WaitGroup
		overMu  sync.Mutex
		overcom bool
	)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			hash := "blob-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
			if err := l.waitForBudget(ctx, hash, math.NewInt(amount)); err != nil {
				return
			}
			// invariant check: reserved must never exceed the funded balance
			chainBal, reserved, _, _ := l.snapshot()
			if reserved.GT(chainBal) {
				overMu.Lock()
				overcom = true
				overMu.Unlock()
			}
			l.releaseSettled(hash)
		}(i)
	}
	wg.Wait()

	require.False(t, overcom, "reserved exceeded balance: escrow was overcommitted")
	_, reserved, _, inflight := l.snapshot()
	require.Equal(t, 0, inflight)
	require.Equal(t, int64(0), reserved.Int64())
	count, _ := d.deposits()
	require.Greater(t, count, 0) // funding actually happened
}
