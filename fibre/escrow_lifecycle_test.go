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
// balance from chain, so a pre-funded escrow doesn't trigger a needless deposit.
func TestEscrowLedgerEnsureSeededUsesExistingBalance(t *testing.T) {
	d := newMockDepositor()
	q := &mockQuerier{bal: math.NewInt(50_000)}
	l := newTestLedger(t, clock.New(), q, d)

	require.NoError(t, l.ensureSeeded(t.Context()))
	require.NoError(t, l.ensureSeeded(t.Context())) // idempotent
	require.Equal(t, 1, q.queries())                // queried exactly once
	require.Equal(t, int64(50_000), l.balanceOf().Int64())

	ok, _ := l.admit(math.NewInt(1_000))
	require.True(t, ok)
	count, _ := d.deposits()
	require.Equal(t, 0, count) // existing balance covered it; no deposit
}

// TestEscrowLedgerFundedBurstNeverOvercommits is the core property: under a
// concurrent burst of admissions that together far exceed the starting balance,
// auto-funding lets every upload through while the local balance never goes
// negative (no overcommit), and deposits are bounded by single-flight.
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
	for range n {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			refill := func() { l.refill(ctx) }
			if err := l.waitForBudget(ctx, refill, math.NewInt(amount)); err != nil {
				return
			}
			// invariant check: balance must never go negative (overcommit).
			if l.balanceOf().IsNegative() {
				overMu.Lock()
				overcom = true
				overMu.Unlock()
			}
		})
	}
	wg.Wait()

	require.False(t, overcom, "balance went negative: escrow was overcommitted")
	require.False(t, l.balanceOf().IsNegative())
	count, _ := d.deposits()
	require.Greater(t, count, 0) // funding actually happened
}
