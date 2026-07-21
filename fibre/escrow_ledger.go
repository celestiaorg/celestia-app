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
	// top-ups of the escrow account. It is opt-in and defaults to false (both via
	// [defaultEscrowConfig] and for a zero-value [EscrowConfig]), since enabling
	// it broadcasts on-chain deposit transactions; set it explicitly to turn
	// auto-funding on.
	AutoFund bool
	// LowWatermark is the local-balance threshold below which a refill is
	// triggered. Sized to cover peak throughput during a deposit's confirmation.
	LowWatermark math.Int
	// HighWatermark is the local-balance target a refill deposits up to.
	HighWatermark math.Int
	// RefillCheckInterval is how often waitForBudget re-checks the balance while
	// blocked waiting for a refill to land. Default 1s.
	RefillCheckInterval time.Duration
	// StartupGracePeriod delays trusting on-chain escrow state after the ledger
	// starts. The local balance is in-memory only, so a client that crashed while
	// promises it signed were still unsettled would, on restart, re-seed from a
	// chain balance that still counts those committed funds as available and
	// could over-sign. Within this window the ledger seeds nothing, admits
	// nothing, and deposits nothing; once it passes, any pre-crash promise has
	// settled or timed out on chain, so the seed is exact. Set it to at least the
	// chain's PaymentPromiseTimeout to close that gap. Zero (the default) disables
	// the grace and seeds immediately, preserving the prior behavior.
	StartupGracePeriod time.Duration
}

// EscrowQuerier reads on-chain escrow state for a signer. It wraps the
// x/fibre Query RPCs.
type EscrowQuerier interface {
	// EscrowBalance returns the signer's available escrow balance — the on-chain
	// balance minus funds locked by pending withdrawals. It returns a zero
	// balance (not an error) when the escrow account does not exist yet.
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
	createdAt time.Time // ledger start; gates the StartupGracePeriod window

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
		createdAt: clk.Now(),
		balance:   math.ZeroInt(),
	}
}

// inStartupGrace reports whether the ledger is still within its post-start grace
// window, during which chain escrow state can't be trusted (see ensureSeeded and
// EscrowConfig.StartupGracePeriod). Always false when StartupGracePeriod is zero,
// which preserves immediate seeding.
func (l *escrowLedger) inStartupGrace() bool {
	if l.cfg.StartupGracePeriod <= 0 {
		return false
	}
	return l.clk.Now().Before(l.createdAt.Add(l.cfg.StartupGracePeriod))
}

// ensureSeeded initializes balance from the chain exactly once (the first time
// the signer is used), so the very first admit sees any pre-existing escrow
// balance instead of triggering an unnecessary deposit. Subsequent calls are a
// cheap no-op.
func (l *escrowLedger) ensureSeeded(ctx context.Context) error {
	if l.seeded.Load() {
		return nil
	}
	// Hold off seeding until the startup grace window passes: a balance queried
	// before then could still count promises signed just before a crash (not yet
	// settled on chain) as available and let us over-sign. balance stays zero
	// meanwhile, so admit refuses and no commitment is made. See
	// EscrowConfig.StartupGracePeriod.
	if l.inStartupGrace() {
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
	// During the startup grace window the balance is deliberately unseeded (zero),
	// so a deposit here would top up against an untrusted baseline. Hold off; the
	// first refill after the window works off the freshly seeded balance.
	if l.inStartupGrace() {
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
	// Within the startup grace window admit can never succeed (seeding is
	// deferred and refill is suppressed), so polling would just spin until ctx
	// expires. Fail fast with an actionable message instead. See
	// EscrowConfig.StartupGracePeriod.
	if l.inStartupGrace() {
		return fmt.Errorf("escrow ledger in startup grace period; admission deferred to avoid over-signing after a restart (see EscrowConfig.StartupGracePeriod)")
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
