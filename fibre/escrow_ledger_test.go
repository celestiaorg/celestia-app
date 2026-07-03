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
	mu    sync.Mutex
	bal   math.Int
	err   error
	count int
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
	entered   chan struct{} // signal each entry
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
	}
}

func newTestLedger(t *testing.T, clk clock.Clock, q EscrowQuerier, d Depositor) *escrowLedger {
	t.Helper()
	return newEscrowLedger("signer1", testEscrowConfig(), clk, q, d, nil)
}

// seedBalance sets the local balance directly, standing in for a completed
// ensureSeeded in tests that exercise admit/credit in isolation.
func seedBalance(l *escrowLedger, v int64) {
	l.mu.Lock()
	l.balance = math.NewInt(v)
	l.mu.Unlock()
}

func TestEscrowLedgerAdmitUntilExhausted(t *testing.T) {
	l := newTestLedger(t, clock.New(), &mockQuerier{}, newMockDepositor())
	seedBalance(l, 100)

	ok, _ := l.admit(math.NewInt(60))
	require.True(t, ok)
	ok, _ = l.admit(math.NewInt(40))
	require.True(t, ok)
	require.Equal(t, int64(0), l.balanceOf().Int64())
	// no budget left
	ok, _ = l.admit(math.NewInt(1))
	require.False(t, ok)
}

func TestEscrowLedgerAdmitReportsLowWatermark(t *testing.T) {
	l := newTestLedger(t, clock.New(), &mockQuerier{}, newMockDepositor())
	seedBalance(l, 1_050) // just above LowWatermark (1_000)

	// admit drops balance to 1_000, still not < LowWatermark
	ok, low := l.admit(math.NewInt(50))
	require.True(t, ok)
	require.False(t, low)

	// next admit crosses below LowWatermark
	ok, low = l.admit(math.NewInt(1))
	require.True(t, ok)
	require.True(t, low)

	// a failed admit still reports the low balance so the caller refills
	ok, low = l.admit(math.NewInt(10_000))
	require.False(t, ok)
	require.True(t, low)
}

func TestEscrowLedgerCreditReturnsBudget(t *testing.T) {
	l := newTestLedger(t, clock.New(), &mockQuerier{}, newMockDepositor())
	seedBalance(l, 100)

	ok, _ := l.admit(math.NewInt(60))
	require.True(t, ok)
	require.Equal(t, int64(40), l.balanceOf().Int64())
	l.credit(math.NewInt(60))
	require.Equal(t, int64(100), l.balanceOf().Int64()) // budget fully returned
}

func TestEscrowLedgerRefillDepositsToHighWatermark(t *testing.T) {
	d := newMockDepositor()
	l := newTestLedger(t, clock.New(), &mockQuerier{}, d)
	seedBalance(l, 500) // below LowWatermark (1000)

	l.refill(t.Context())
	count, total := d.deposits()
	require.Equal(t, 1, count)
	// deposits up to HighWatermark: 10_000 - 500 = 9_500
	require.Equal(t, int64(9_500), total.Int64())
	require.Equal(t, int64(10_000), l.balanceOf().Int64())

	// at HighWatermark now: no further deposit
	l.refill(t.Context())
	count, _ = d.deposits()
	require.Equal(t, 1, count)
}

func TestEscrowLedgerRefillDisabled(t *testing.T) {
	d := newMockDepositor()
	l := newTestLedger(t, clock.New(), &mockQuerier{}, d)
	l.cfg.AutoFund = false
	seedBalance(l, 0)

	l.refill(t.Context())
	count, _ := d.deposits()
	require.Equal(t, 0, count)
}

func TestEscrowLedgerRefillDepositErrorLeavesBalance(t *testing.T) {
	d := newMockDepositor()
	d.err = errors.New("broadcast failed")
	l := newTestLedger(t, clock.New(), &mockQuerier{}, d)
	seedBalance(l, 0)

	l.refill(t.Context())
	// deposit failed: balance must not be credited (never overstate).
	require.Equal(t, int64(0), l.balanceOf().Int64())
}

func TestEscrowLedgerRefillSingleFlight(t *testing.T) {
	d := newMockDepositor()
	d.block = make(chan struct{})
	l := newTestLedger(t, clock.New(), &mockQuerier{}, d)
	seedBalance(l, 0)

	// first refill enters DepositToEscrow and blocks, holding the refilling flag
	done := make(chan struct{})
	go func() { l.refill(t.Context()); close(done) }()
	<-d.entered // ensure the first deposit is in progress

	// concurrent refill must be skipped (CAS fails), not stacked
	l.refill(t.Context())
	count, _ := d.deposits()
	require.Equal(t, 0, count) // first one hasn't completed yet, second skipped

	close(d.block)
	<-done
	count, _ = d.deposits()
	require.Equal(t, 1, count) // exactly one deposit total
}

func TestEscrowLedgerWaitForBudgetUnblocksAfterRefill(t *testing.T) {
	d := newMockDepositor()
	l := newTestLedger(t, clock.New(), &mockQuerier{}, d)
	seedBalance(l, 0) // nothing available, below LowWatermark

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	refill := func() { l.refill(ctx) }
	// amount fits within HighWatermark, so the triggered refill unblocks it
	require.NoError(t, l.waitForBudget(ctx, refill, math.NewInt(5_000)))
	require.Equal(t, int64(5_000), l.balanceOf().Int64()) // 10_000 refilled − 5_000 admitted
	count, _ := d.deposits()
	require.GreaterOrEqual(t, count, 1)
}

// A payment larger than LowWatermark but within HighWatermark must not spin:
// waitForBudget refills up to HighWatermark, which covers it.
func TestEscrowLedgerWaitForBudgetRefillsAboveLowWatermark(t *testing.T) {
	d := newMockDepositor()
	l := newTestLedger(t, clock.New(), &mockQuerier{}, d)
	// balance (2_000) is above LowWatermark (1_000) but below the requested
	// amount (5_000) — admit fails, and the refill tops up to HighWatermark.
	seedBalance(l, 2_000)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	refill := func() { l.refill(ctx) }
	require.NoError(t, l.waitForBudget(ctx, refill, math.NewInt(5_000)))
	count, _ := d.deposits()
	require.GreaterOrEqual(t, count, 1)
}

func TestEscrowLedgerWaitForBudgetRespectsContext(t *testing.T) {
	l := newTestLedger(t, clock.New(), &mockQuerier{}, newMockDepositor())
	l.cfg.AutoFund = false // no refills, so budget never appears
	seedBalance(l, 0)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	err := l.waitForBudget(ctx, func() {}, math.NewInt(100))
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestEscrowLedgerWaitForBudgetFailsFastOnOversizedPayment(t *testing.T) {
	d := newMockDepositor()
	l := newTestLedger(t, clock.New(), &mockQuerier{}, d)
	seedBalance(l, 0)

	// amount exceeds HighWatermark (10_000): auto-funding can never satisfy it,
	// so waitForBudget must fail immediately rather than spin until ctx expires.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := l.waitForBudget(ctx, func() { l.refill(ctx) }, math.NewInt(10_001))
	require.Error(t, err)
	require.NotErrorIs(t, err, context.DeadlineExceeded)
	count, _ := d.deposits()
	require.Equal(t, 0, count) // no pointless deposits
}

func TestEscrowLedgerConcurrentAdmitCredit(t *testing.T) {
	l := newTestLedger(t, clock.New(), &mockQuerier{}, newMockDepositor())
	seedBalance(l, 1_000_000)

	var wg sync.WaitGroup
	for i := range 200 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if ok, _ := l.admit(math.NewInt(10)); ok && i%2 == 0 {
				l.credit(math.NewInt(10)) // half abort before sign, half stay committed
			}
		}(i)
	}
	wg.Wait()
	// 200 admits of 10, 100 credited back: balance = 1_000_000 − 100*10 = 999_000.
	require.Equal(t, int64(999_000), l.balanceOf().Int64())
	// balance never went negative (structurally guaranteed by admit).
	require.False(t, l.balanceOf().IsNegative())
}
