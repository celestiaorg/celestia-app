package fibre

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"cosmossdk.io/math"
	clock "github.com/filecoin-project/go-clock"
)

// EscrowConfig controls the client-side escrow auto-funding behavior. See
// [ClientConfig] for how defaults are derived. When AutoFund is false the ledger
// still admits against the local balance but never deposits.
type EscrowConfig struct {
	// AutoFund enables client-side escrow admission control plus background
	// top-ups of the escrow account. It defaults to true via
	// [defaultEscrowConfig]; note a zero-value [EscrowConfig] (AutoFund false)
	// disables auto-funding, so build configs with the constructors.
	AutoFund bool
	// LowWatermark is the local-balance threshold below which a refill is
	// triggered. Sized to cover peak throughput during a deposit's confirmation.
	LowWatermark math.Int
	// HighWatermark is the local-balance target a refill deposits up to.
	HighWatermark math.Int
	// RefillCheckInterval is how often waitForBudget re-checks the balance while
	// blocked waiting for a refill to land. Default 1s.
	RefillCheckInterval time.Duration
}

// EscrowQuerier reads on-chain escrow state for a signer. It wraps the
// x/fibre Query RPCs.
type EscrowQuerier interface {
	// EscrowBalance returns the total escrow Balance for the signer. It returns
	// a zero balance (not an error) when the escrow account does not exist yet.
	EscrowBalance(ctx context.Context, signer string) (math.Int, error)
}

// Depositor tops up the escrow account by broadcasting a MsgDepositToEscrow for
// the signer and waiting for inclusion.
type Depositor interface {
	DepositToEscrow(ctx context.Context, signer string, amount math.Int) error
}

// escrowLedger is a single signer's client-side escrow budget: one number,
// guarded by mu. balance is the on-chain escrow balance minus the funds already
// committed to signed-but-not-yet-settled promises. An upload is admitted only
// when balance covers its payment; balance is decremented on admission and
// credited back only if the upload is aborted before its promise is signed —
// once signed, the money is committed (the settle or timeout path debits it on
// chain), so it is never returned.
//
// Because balance is only ever credited by a real deposit or by an
// abort-before-sign (never by a phantom amount), it can never be overstated, so
// concurrent uploads can never collectively overcommit the escrow — the one
// invariant a per-call balance check cannot provide. A background refill keeps
// the balance topped up; deposits are single-flighted so concurrent callers
// never stack them.
type escrowLedger struct {
	signer    string
	cfg       EscrowConfig
	clk       clock.Clock
	querier   EscrowQuerier
	depositor Depositor
	log       *slog.Logger

	mu      sync.Mutex
	balance math.Int // = on-chain escrow − committed (signed) promises

	seeded    atomic.Bool // balance initialized from chain; lock-free fast path
	seedMu    sync.Mutex  // blocks late callers until the one-time seeding completes
	refilling atomic.Bool // single-flights DepositToEscrow so callers don't double-fund
}

// newEscrowLedger builds a ledger for signer. balance starts at zero and is
// corrected by the first ensureSeeded; admit conservatively refuses until then.
func newEscrowLedger(signer string, cfg EscrowConfig, clk clock.Clock, q EscrowQuerier, d Depositor, log *slog.Logger) *escrowLedger {
	return &escrowLedger{
		signer:    signer,
		cfg:       cfg,
		clk:       clk,
		querier:   q,
		depositor: d,
		log:       log,
		balance:   math.ZeroInt(),
	}
}

// ensureSeeded initializes balance from the chain exactly once (the first time
// the signer is used), so the very first admit sees any pre-existing escrow
// balance instead of triggering an unnecessary deposit. Subsequent calls are a
// cheap no-op.
func (l *escrowLedger) ensureSeeded(ctx context.Context) error {
	if l.seeded.Load() {
		return nil
	}
	l.seedMu.Lock()
	defer l.seedMu.Unlock()
	if l.seeded.Load() {
		return nil
	}
	bal, err := l.querier.EscrowBalance(ctx, l.signer)
	if err != nil {
		return fmt.Errorf("query escrow balance: %w", err)
	}
	l.mu.Lock()
	l.balance = bal
	l.mu.Unlock()
	// Publish seeded last, after balance is in place, so a goroutine that sees
	// seeded via the lock-free fast path also sees the seeded balance.
	l.seeded.Store(true)
	return nil
}

// admit decrements balance by amount if it fits and returns ok=true. It also
// reports whether the balance is below LowWatermark (low), so the caller can
// kick a refill off the hot path — reported on both the success and failure
// paths, since a failed admit is exactly when a refill is most needed.
func (l *escrowLedger) admit(amount math.Int) (ok, low bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.balance.LT(amount) {
		return false, l.balance.LT(l.cfg.LowWatermark)
	}
	l.balance = l.balance.Sub(amount)
	return true, l.balance.LT(l.cfg.LowWatermark)
}

// credit returns amount to the balance. Called only when an upload is aborted
// before its promise is signed — the funds were never committed, so returning
// them is safe. After signing, the money is committed and must never be
// credited back (doing so would overstate balance and risk overcommit).
func (l *escrowLedger) credit(amount math.Int) {
	l.mu.Lock()
	l.balance = l.balance.Add(amount)
	l.mu.Unlock()
}

// balanceOf returns the current local balance (for tests/diagnostics).
func (l *escrowLedger) balanceOf() math.Int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.balance
}

// refill deposits up to HighWatermark when the balance has fallen below it. It
// is a no-op when AutoFund is off, when already at/above HighWatermark, or when
// another refill is in flight (single-flighted via the refilling flag) so
// concurrent callers never stack deposits.
//
// The deposit is applied to balance as a delta once confirmed. This is
// unconditionally correct because nothing else ever overwrites balance from
// chain during operation — the only chain read is the one-time seeding — so the
// delta can never double-count a concurrent re-read.
func (l *escrowLedger) refill(ctx context.Context) {
	if !l.cfg.AutoFund {
		return
	}
	if !l.refilling.CompareAndSwap(false, true) {
		return // another refill is already running
	}
	defer l.refilling.Store(false)

	l.mu.Lock()
	deposit := l.cfg.HighWatermark.Sub(l.balance)
	l.mu.Unlock()
	if !deposit.IsPositive() {
		return
	}

	if err := l.depositor.DepositToEscrow(ctx, l.signer, deposit); err != nil {
		if l.log != nil {
			l.log.Warn("escrow refill failed", "signer", l.signer, "err", err)
		}
		return
	}
	l.mu.Lock()
	l.balance = l.balance.Add(deposit)
	l.mu.Unlock()
	if l.log != nil {
		l.log.Debug("escrow refilled", "signer", l.signer, "deposit", deposit.String())
	}
}

// waitForBudget blocks until amount can be admitted for the caller, triggering
// refills via refill and polling on RefillCheckInterval, bounded by ctx. It is
// the hot-path fallback when admit fails because the budget was momentarily
// exhausted. On success the amount has already been decremented from balance.
func (l *escrowLedger) waitForBudget(ctx context.Context, refill func(), amount math.Int) error {
	// A single payment larger than HighWatermark can never be satisfied by
	// auto-funding (refills only top up to HighWatermark), so polling would just
	// spin until ctx expires. Surface the misconfiguration immediately instead.
	if l.cfg.AutoFund && l.cfg.HighWatermark.LT(amount) {
		return fmt.Errorf("escrow payment %s exceeds high_watermark %s: raise HighWatermark to admit blobs this large", amount, l.cfg.HighWatermark)
	}
	for {
		if ok, _ := l.admit(amount); ok {
			return nil
		}
		refill()
		// Re-check immediately: when refill is synchronous (the hot-path caller
		// deposits inline) the budget is already available, so returning here
		// avoids sleeping a whole RefillCheckInterval for nothing.
		if ok, _ := l.admit(amount); ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-l.clk.After(l.cfg.RefillCheckInterval):
		}
	}
}
