package fibre

import (
	"testing"

	"cosmossdk.io/math"
	clock "github.com/filecoin-project/go-clock"
	"github.com/stretchr/testify/require"
)

// TestEscrowLedgerRefillReconcileDoubleCount is a regression guard for the
// refill/reconcile double-count. It deterministically forces the interleaving:
//
//	A: maybeRefill computes deposit, broadcasts+confirms it (funds now on-chain)
//	B: reconcile reads the post-deposit balance and sets chainBal = balance
//	A: resumes to apply the deposit
//
// Before the fix, step A additively did chainBal += deposit, leaving chainBal at
// 2x the true on-chain balance — overstating available budget and letting the
// escrow be overcommitted. The reconcileGen guard makes A skip the additive when
// a reconcile intervened, so chainBal stays equal to the true on-chain balance.
func TestEscrowLedgerRefillReconcileDoubleCount(t *testing.T) {
	d := newMockDepositor()
	d.block = make(chan struct{}) // hold DepositToEscrow open mid-flight
	q := &mockQuerier{bal: math.NewInt(0)}
	l := newTestLedger(t, clock.New(), q, d)
	l.chainBal = math.NewInt(0) // below LowWatermark(1000) -> refill fires

	const deposit = int64(10_000) // HighWatermark - 0

	// A: start the refill; it will block inside DepositToEscrow.
	done := make(chan error, 1)
	go func() { done <- l.maybeRefill(t.Context()) }()
	<-d.entered // A is now inside DepositToEscrow, holding refillMu

	// Simulate the deposit landing on-chain: the queried balance now reflects it.
	q.set(math.NewInt(deposit))

	// B: reconcile runs while A's deposit is still in flight and reads the
	// post-deposit balance as ground truth.
	require.NoError(t, l.reconcile(t.Context()))
	require.Equal(t, deposit, l.chainBal.Int64(), "reconcile should have set chainBal to the true on-chain balance")

	// A: let the deposit return; maybeRefill now adds `deposit` on top.
	close(d.block)
	require.NoError(t, <-done)

	// Ground truth on-chain balance is `deposit`. If chainBal is 2*deposit,
	// the deposit was counted twice and available budget is overstated.
	require.Equal(t, deposit, l.chainBal.Int64(),
		"chainBal double-counted the deposit: on-chain balance is %d but ledger thinks %d", deposit, l.chainBal.Int64())
}
