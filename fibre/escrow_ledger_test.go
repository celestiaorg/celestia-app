package fibre

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"cosmossdk.io/math"
	clock "github.com/filecoin-project/go-clock"
	"github.com/stretchr/testify/require"
)

// mockQuerier returns a balance the test controls and counts queries.
type mockQuerier struct {
	mu      sync.Mutex
	bal     math.Int
	err     error
	count   int
	timeout time.Duration // chain PaymentPromiseTimeout; 0 means "report 1h"
}

func (m *mockQuerier) PaymentPromiseTimeout(_ context.Context) (time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.timeout == 0 {
		return time.Hour, nil
	}
	return m.timeout, nil
}

func (m *mockQuerier) set(bal math.Int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bal = bal
}

func (m *mockQuerier) queries() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.count
}

func (m *mockQuerier) EscrowBalance(_ context.Context, _ string) (math.Int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.count++
	if m.err != nil {
		return math.ZeroInt(), m.err
	}
	return m.bal, nil
}

// mockDepositor records deposits and can be made to block or fail.
type mockDepositor struct {
	mu        sync.Mutex
	count     int
	total     math.Int
	err       error
	block     chan struct{} // if non-nil, DepositToEscrow waits on it
	entered   chan struct{} // closed-style signal each entry
	onDeposit func(amount math.Int)
}

func newMockDepositor() *mockDepositor {
	return &mockDepositor{total: math.ZeroInt(), entered: make(chan struct{}, 16)}
}

func (m *mockDepositor) DepositToEscrow(_ context.Context, _ string, amount math.Int) error {
	select {
	case m.entered <- struct{}{}:
	default:
	}
	if m.block != nil {
		<-m.block
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.count++
	m.total = m.total.Add(amount)
	if m.onDeposit != nil {
		m.onDeposit(amount)
	}
	return nil
}

func (m *mockDepositor) deposits() (int, math.Int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.count, m.total
}

func testEscrowConfig() EscrowConfig {
	return EscrowConfig{
		AutoFund:            true,
		LowWatermark:        math.NewInt(1_000),
		HighWatermark:       math.NewInt(10_000),
		RefillCheckInterval: 5 * time.Millisecond,
		ReconcileInterval:   time.Second,
		ReservationTTL:      time.Hour,
	}
}

func newTestLedger(t *testing.T, clk clock.Clock, q EscrowQuerier, d Depositor) *escrowLedger {
	t.Helper()
	return newEscrowLedger("signer1", testEscrowConfig(), clk, q, d, nil)
}

func TestEscrowLedgerReserveUntilExhausted(t *testing.T) {
	l := newTestLedger(t, clock.New(), &mockQuerier{}, newMockDepositor())
	l.chainBal = math.NewInt(100) // seed directly for the unit

	require.True(t, l.reserve("a", math.NewInt(60)))
	require.True(t, l.reserve("b", math.NewInt(40)))
	require.Equal(t, int64(0), l.available().Int64())
	// no budget left
	require.False(t, l.reserve("c", math.NewInt(1)))
}

func TestEscrowLedgerReserveIdempotent(t *testing.T) {
	l := newTestLedger(t, clock.New(), &mockQuerier{}, newMockDepositor())
	l.chainBal = math.NewInt(100)

	require.True(t, l.reserve("a", math.NewInt(60)))
	require.True(t, l.reserve("a", math.NewInt(60))) // same hash, no double charge
	require.Equal(t, int64(40), l.available().Int64())
}

func TestEscrowLedgerReleaseUnsettledReturnsBudget(t *testing.T) {
	l := newTestLedger(t, clock.New(), &mockQuerier{}, newMockDepositor())
	l.chainBal = math.NewInt(100)

	require.True(t, l.reserve("a", math.NewInt(60)))
	l.releaseUnsettled("a")
	require.Equal(t, int64(100), l.available().Int64()) // budget fully returned
	// idempotent / unknown hash is a no-op
	l.releaseUnsettled("a")
	l.releaseUnsettled("missing")
	require.Equal(t, int64(100), l.available().Int64())
}

func TestEscrowLedgerReleaseSettledShrinksBoth(t *testing.T) {
	l := newTestLedger(t, clock.New(), &mockQuerier{}, newMockDepositor())
	l.chainBal = math.NewInt(100)

	require.True(t, l.reserve("a", math.NewInt(60)))
	require.Equal(t, int64(40), l.available().Int64())
	l.releaseSettled("a") // paid: funds left escrow
	// available is unchanged (the budget was already committed)...
	require.Equal(t, int64(40), l.available().Int64())
	// ...because chainBal dropped by the payment too.
	require.Equal(t, int64(40), l.chainBal.Int64())
	require.Equal(t, int64(0), l.reserved.Int64())
}

func TestEscrowLedgerReconcileSyncsBalanceAndExpiresTTL(t *testing.T) {
	mock := clock.NewMock()
	q := &mockQuerier{bal: math.NewInt(500)}
	l := newTestLedger(t, mock, q, newMockDepositor())

	// initial reconcile sets chainBal from chain
	require.NoError(t, l.reconcile(t.Context()))
	require.Equal(t, int64(500), l.chainBal.Int64())

	require.True(t, l.reserve("old", math.NewInt(100)))
	require.Equal(t, int64(400), l.available().Int64())

	// advance past the TTL; reconcile should drop the stale reservation
	mock.Add(l.cfg.ReservationTTL + time.Minute)
	q.set(math.NewInt(450)) // e.g. some unrelated settlement happened
	require.NoError(t, l.reconcile(t.Context()))
	require.Equal(t, int64(450), l.chainBal.Int64())
	require.Equal(t, int64(0), l.reserved.Int64())
	require.Equal(t, int64(450), l.available().Int64())
}

func TestEscrowLedgerReconcilePropagatesQueryError(t *testing.T) {
	q := &mockQuerier{err: errors.New("rpc down")}
	l := newTestLedger(t, clock.New(), q, newMockDepositor())
	require.Error(t, l.reconcile(t.Context()))
}

func TestEscrowLedgerMaybeRefillDepositsToHighWatermark(t *testing.T) {
	d := newMockDepositor()
	l := newTestLedger(t, clock.New(), &mockQuerier{}, d)
	l.chainBal = math.NewInt(500) // below LowWatermark (1000)

	require.NoError(t, l.maybeRefill(t.Context()))
	count, total := d.deposits()
	require.Equal(t, 1, count)
	// deposits up to HighWatermark: 10_000 - 500 = 9_500
	require.Equal(t, int64(9_500), total.Int64())
	require.Equal(t, int64(10_000), l.available().Int64())

	// above LowWatermark now: no further deposit
	require.NoError(t, l.maybeRefill(t.Context()))
	count, _ = d.deposits()
	require.Equal(t, 1, count)
}

func TestEscrowLedgerMaybeRefillDisabled(t *testing.T) {
	d := newMockDepositor()
	l := newTestLedger(t, clock.New(), &mockQuerier{}, d)
	l.cfg.AutoFund = false
	l.chainBal = math.NewInt(0)

	require.NoError(t, l.maybeRefill(t.Context()))
	count, _ := d.deposits()
	require.Equal(t, 0, count)
}

func TestEscrowLedgerMaybeRefillSingleFlight(t *testing.T) {
	d := newMockDepositor()
	d.block = make(chan struct{})
	l := newTestLedger(t, clock.New(), &mockQuerier{}, d)
	l.chainBal = math.NewInt(0)

	// first refill enters DepositToEscrow and blocks, holding refillMu
	done := make(chan error, 1)
	go func() { done <- l.maybeRefill(t.Context()) }()
	<-d.entered // ensure the first deposit is in progress

	// concurrent refill must be skipped (TryLock fails), not stacked
	require.NoError(t, l.maybeRefill(t.Context()))
	count, _ := d.deposits()
	require.Equal(t, 0, count) // first one hasn't completed yet, second skipped

	close(d.block)
	require.NoError(t, <-done)
	count, _ = d.deposits()
	require.Equal(t, 1, count) // exactly one deposit total
}

func TestEscrowLedgerWaitForBudgetUnblocksAfterRefill(t *testing.T) {
	d := newMockDepositor()
	q := &mockQuerier{}
	l := newTestLedger(t, clock.New(), q, d)
	l.chainBal = math.NewInt(0) // nothing available, below LowWatermark

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	// amount fits within HighWatermark, so the triggered refill unblocks it
	require.NoError(t, l.waitForBudget(ctx, "a", math.NewInt(5_000)))
	require.Equal(t, int64(5_000), l.reserved.Int64())
	count, _ := d.deposits()
	require.GreaterOrEqual(t, count, 1)
}

func TestEscrowLedgerWaitForBudgetRespectsContext(t *testing.T) {
	l := newTestLedger(t, clock.New(), &mockQuerier{}, newMockDepositor())
	l.cfg.AutoFund = false // no refills, so budget never appears
	l.chainBal = math.NewInt(0)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	err := l.waitForBudget(ctx, "a", math.NewInt(100))
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestEscrowLedgerWaitForBudgetFailsFastOnOversizedPayment(t *testing.T) {
	d := newMockDepositor()
	l := newTestLedger(t, clock.New(), &mockQuerier{}, d)
	l.chainBal = math.NewInt(0)

	// amount exceeds HighWatermark (10_000): auto-funding can never satisfy it,
	// so waitForBudget must fail immediately rather than spin until ctx expires.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := l.waitForBudget(ctx, "big", math.NewInt(10_001))
	require.Error(t, err)
	require.NotErrorIs(t, err, context.DeadlineExceeded)
	count, _ := d.deposits()
	require.Equal(t, 0, count) // no pointless deposits
}

func TestEscrowLedgerEnsureSeededBumpsTTLFromChain(t *testing.T) {
	q := &mockQuerier{bal: math.NewInt(0), timeout: 2 * time.Hour}
	l := newTestLedger(t, clock.New(), q, newMockDepositor())
	require.Equal(t, time.Hour, l.ttl) // configured TTL before seeding

	require.NoError(t, l.ensureSeeded(t.Context()))
	// chain timeout (2h) + margin exceeds the configured 1h, so the ledger adopts it.
	require.Equal(t, 2*time.Hour+reservationTTLMargin, l.ttl)
}

func TestEscrowLedgerEnsureSeededKeepsConfiguredTTLWhenChainLower(t *testing.T) {
	q := &mockQuerier{bal: math.NewInt(0), timeout: time.Minute}
	l := newTestLedger(t, clock.New(), q, newMockDepositor())

	require.NoError(t, l.ensureSeeded(t.Context()))
	// chain timeout (1m) + margin is below the configured 1h, so keep the config.
	require.Equal(t, time.Hour, l.ttl)
}

func TestEscrowLedgerConcurrentReserveRelease(t *testing.T) {
	l := newTestLedger(t, clock.New(), &mockQuerier{}, newMockDepositor())
	l.chainBal = math.NewInt(1_000_000)

	var wg sync.WaitGroup
	for i := range 200 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			hash := string(rune('a'+i%26)) + string(rune('0'+i/26))
			if l.reserve(hash, math.NewInt(10)) {
				if i%2 == 0 {
					l.releaseSettled(hash)
				} else {
					l.releaseUnsettled(hash)
				}
			}
		}(i)
	}
	wg.Wait()
	// after all settled/unsettled, no reservation should leak
	_, reserved, _, inflight := l.snapshot()
	require.Equal(t, 0, inflight)
	require.Equal(t, int64(0), reserved.Int64())
}
