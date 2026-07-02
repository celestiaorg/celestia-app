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
// [ClientConfig] for how defaults are derived. When AutoFund is false the
// ledger still tracks reservations but never deposits, and the background
// refill loop is not started.
type EscrowConfig struct {
	// AutoFund enables client-side escrow admission control plus background
	// top-ups of the escrow account. It defaults to true via
	// [defaultEscrowConfig]; note a zero-value [EscrowConfig] (AutoFund false)
	// disables escrow admission entirely, so build configs with the constructors.
	AutoFund bool
	// LowWatermark is the available-budget threshold below which a refill is
	// triggered. Sized to cover peak throughput during a deposit's confirmation.
	LowWatermark math.Int
	// HighWatermark is the available-budget target a refill deposits up to.
	HighWatermark math.Int
	// RefillCheckInterval is how often the background loop checks the budget and
	// how often waitForBudget polls. Default 1s.
	RefillCheckInterval time.Duration
	// ReconcileInterval is how often the ledger re-syncs chainBal from chain.
	ReconcileInterval time.Duration
	// ReservationTTL is how long a reservation may live before reconcile drops
	// it as resolved. Must be >= the module's PaymentPromiseTimeout plus margin.
	ReservationTTL time.Duration
}

// EscrowQuerier reads on-chain escrow state for a signer. It wraps the
// x/fibre Query RPCs.
type EscrowQuerier interface {
	// EscrowBalance returns the total escrow Balance for the signer. It returns
	// a zero balance (not an error) when the escrow account does not exist yet.
	EscrowBalance(ctx context.Context, signer string) (math.Int, error)
	// PaymentPromiseTimeout returns the chain's current payment-promise timeout
	// param: the point by which a signed promise is necessarily settled or dead.
	// The ledger uses it to size the reservation TTL so reconcile never drops a
	// reservation whose promise is still settleable on-chain.
	PaymentPromiseTimeout(ctx context.Context) (time.Duration, error)
}

// Depositor tops up the escrow account by broadcasting a MsgDepositToEscrow for
// the signer and waiting for inclusion.
type Depositor interface {
	DepositToEscrow(ctx context.Context, signer string, amount math.Int) error
}

// reservation is a single in-flight promise's hold on escrow funds.
type reservation struct {
	amount   math.Int
	signedAt time.Time
}

// escrowLedger is the client-side accounting for a single escrow signer. It lets
// the upload hot path decide locally — without a chain round-trip — whether the
// next PaymentPromise fits within the escrow, by tracking the sum of all
// in-flight (signed-but-not-yet-settled) promises against the last known chain
// balance. A background loop keeps the balance topped up.
//
// The invariant it enforces is the one neither the server nor a per-call balance
// check can: a promise is admitted only when balance >= Σ(in-flight) + this one,
// so concurrent promises can never collectively overcommit the escrow.
//
// One ledger instance owns one signer and is shared by all goroutines that
// upload for it. All mutable accounting is guarded by mu, held only for short,
// non-blocking critical sections (never across a chain round-trip). The two
// network operations are kept off mu: a deposit is single-flighted by the
// refilling flag, and the one-time seeding reconcile is serialized by seedMu so
// late callers wait for it to finish rather than racing an unseeded balance.
type escrowLedger struct {
	signer    string
	cfg       EscrowConfig
	clk       clock.Clock
	querier   EscrowQuerier
	depositor Depositor
	log       *slog.Logger

	mu            sync.Mutex
	chainBal      math.Int               // escrow Balance as of the last reconcile/deposit
	reserved      math.Int               // Σ amounts of in-flight promises
	inflight      map[string]reservation // keyed by unique per-upload reservation id
	ttl           time.Duration          // effective reservation TTL (>= chain PaymentPromiseTimeout)
	lastReconcile time.Time              // last successful reconcile
	reconcileGen  uint64                 // bumped every time reconcile overwrites chainBal from chain

	seeded    atomic.Bool // chainBal initialized from chain; lock-free double-check in ensureSeeded
	refilling atomic.Bool // single-flights DepositToEscrow so concurrent callers don't double-fund
	seedMu    sync.Mutex  // blocks late callers until the one-time seeding reconcile completes
}

// newEscrowLedger builds a ledger for signer. chainBal starts at zero and is
// corrected on the first reconcile; reserve conservatively refuses until then.
func newEscrowLedger(signer string, cfg EscrowConfig, clk clock.Clock, q EscrowQuerier, d Depositor, log *slog.Logger) *escrowLedger {
	return &escrowLedger{
		signer:    signer,
		cfg:       cfg,
		clk:       clk,
		querier:   q,
		depositor: d,
		log:       log,
		chainBal:  math.ZeroInt(),
		reserved:  math.ZeroInt(),
		inflight:  make(map[string]reservation),
		ttl:       cfg.ReservationTTL,
	}
}

// reservationTTLMargin is added to the chain's PaymentPromiseTimeout when sizing
// the effective reservation TTL, so a promise is comfortably resolved before its
// reservation can be reclaimed by reconcile.
const reservationTTLMargin = 10 * time.Minute

// availableLocked is chainBal - reserved. May be negative if external activity
// (e.g. a withdrawal) drained the escrow below current reservations; callers
// treat any non-positive value as "no budget".
func (l *escrowLedger) availableLocked() math.Int {
	return l.chainBal.Sub(l.reserved)
}

// available returns the currently spendable budget (chainBal - reserved).
func (l *escrowLedger) available() math.Int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.availableLocked()
}

// reserve attempts to hold amount for the promise identified by hash. It returns
// true if the reservation was taken (or already existed — reserve is idempotent
// per hash), false if the local available budget can't cover it. A false result
// means the caller should waitForBudget or back off.
func (l *escrowLedger) reserve(hash string, amount math.Int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.inflight[hash]; ok {
		return true
	}
	if l.availableLocked().LT(amount) {
		return false
	}
	l.reserved = l.reserved.Add(amount)
	l.inflight[hash] = reservation{amount: amount, signedAt: l.clk.Now()}
	return true
}

// release drops the reservation for hash (no-op if unknown), always shrinking
// reserved. When debitChain is set the amount is also subtracted from chainBal,
// reflecting funds that have left the escrow. See releaseSettled/releaseUnsettled.
func (l *escrowLedger) release(hash string, debitChain bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.inflight[hash]
	if !ok {
		return
	}
	l.reserved = l.reserved.Sub(r.amount)
	if debitChain {
		l.chainBal = l.chainBal.Sub(r.amount)
	}
	delete(l.inflight, hash)
}

// releaseSettled drops the reservation for a promise that was paid on-chain. The
// funds have left the escrow, so both reserved and chainBal shrink by the
// amount, leaving available unchanged (the budget was already committed).
//
// If a reconcile happened to read the post-settlement balance before this call,
// chainBal is briefly understated (debited twice); this is conservative — it can
// only refuse budget, never overcommit — and the next reconcile restores ground
// truth.
func (l *escrowLedger) releaseSettled(hash string) { l.release(hash, true) }

// releaseUnsettled drops the reservation for a promise that was aborted before
// settlement (upload/broadcast failed) and whose funds were never debited. Only
// reserved shrinks, returning the budget to available.
func (l *escrowLedger) releaseUnsettled(hash string) { l.release(hash, false) }

// reconcile re-reads the escrow balance from chain (ground truth, reflecting all
// settlements and withdrawals) and drops reservations older than ReservationTTL,
// by which point a promise is necessarily resolved (settled or dead). This both
// initializes chainBal and corrects any drift from missed releases.
func (l *escrowLedger) reconcile(ctx context.Context) error {
	bal, err := l.querier.EscrowBalance(ctx, l.signer)
	if err != nil {
		return fmt.Errorf("query escrow balance: %w", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clk.Now()
	for h, r := range l.inflight {
		if now.Sub(r.signedAt) > l.ttl {
			l.reserved = l.reserved.Sub(r.amount)
			delete(l.inflight, h)
		}
	}
	l.chainBal = bal
	// Signal to any in-flight refill that chainBal was just overwritten from
	// chain, so it must not additively re-apply its deposit (which this read may
	// already include) and double-count it. See refill.
	l.reconcileGen++
	return nil
}

// refill deposits up to HighWatermark when available has fallen below trigger.
// It is a no-op when AutoFund is off, when already at/above trigger, or when
// another refill is in flight (single-flighted via the refilling flag), so
// concurrent callers never stack deposits. Background upkeep passes LowWatermark;
// waitForBudget passes the payment amount, so a single payment between the
// watermarks still triggers a refill instead of spinning.
func (l *escrowLedger) refill(ctx context.Context, trigger math.Int) error {
	if !l.cfg.AutoFund {
		return nil
	}
	if !l.refilling.CompareAndSwap(false, true) {
		return nil // another refill is already running
	}
	defer l.refilling.Store(false)

	l.mu.Lock()
	avail := l.availableLocked()
	deposit := l.cfg.HighWatermark.Sub(avail)
	genBefore := l.reconcileGen
	l.mu.Unlock()

	if avail.GTE(trigger) || !deposit.IsPositive() {
		return nil
	}

	if err := l.depositor.DepositToEscrow(ctx, l.signer, deposit); err != nil {
		return fmt.Errorf("deposit to escrow: %w", err)
	}
	l.mu.Lock()
	if l.reconcileGen == genBefore {
		// No reconcile ran while the deposit was in flight, so chainBal is still
		// our pre-deposit ground truth: apply the deposit as a delta.
		l.chainBal = l.chainBal.Add(deposit)
	}
	// Otherwise a concurrent reconcile already overwrote chainBal from chain.
	// Its read may or may not include this just-landed deposit, so re-applying
	// the delta could double-count it and overstate available budget (risking
	// overcommit — the one invariant the ledger must never break). We skip the
	// additive instead; any resulting under-count is conservative (it can only
	// refuse budget, never overcommit) and the next reconcile restores ground truth.
	l.mu.Unlock()
	if l.log != nil {
		l.log.Debug("escrow refilled", "signer", l.signer, "deposit", deposit.String())
	}
	return nil
}

// waitForBudget blocks until amount can be reserved for hash, triggering refills
// and polling on RefillCheckInterval, bounded by ctx. It is the hot-path
// fallback when reserve fails because the budget was momentarily exhausted.
func (l *escrowLedger) waitForBudget(ctx context.Context, hash string, amount math.Int) error {
	// A single payment larger than HighWatermark can never be satisfied by
	// auto-funding (refills only top up to HighWatermark), so polling would just
	// spin until ctx expires. Surface the misconfiguration immediately instead.
	if l.cfg.AutoFund && l.cfg.HighWatermark.LT(amount) {
		return fmt.Errorf("escrow payment %s exceeds high_watermark %s: raise HighWatermark to admit blobs this large", amount, l.cfg.HighWatermark)
	}
	for {
		if l.reserve(hash, amount) {
			return nil
		}
		// Refill toward the payment amount, not just the LowWatermark: a payment
		// larger than LowWatermark (e.g. a low custom watermark) would otherwise
		// never trip the background refill gate and spin until ctx expires.
		if err := l.refill(ctx, amount); err != nil {
			return err
		}
		if l.reserve(hash, amount) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-l.clk.After(l.cfg.RefillCheckInterval):
		}
	}
}

// ensureSeeded initializes chainBal from the chain exactly once (the first time
// the signer is used), so the very first reserve sees any pre-existing escrow
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

	if err := l.reconcile(ctx); err != nil {
		return err
	}

	// Best-effort: pin the effective TTL to the chain's actual PaymentPromiseTimeout
	// (which governance may have raised above the configured default) so reconcile
	// can't reclaim a reservation whose promise is still settleable. A failure here
	// leaves the configured TTL in place; it must not block seeding.
	ttl := l.cfg.ReservationTTL
	if timeout, err := l.querier.PaymentPromiseTimeout(ctx); err != nil {
		if l.log != nil {
			l.log.Warn("escrow payment-promise timeout query failed; using configured TTL", "signer", l.signer, "err", err)
		}
	} else if chainTTL := timeout + reservationTTLMargin; chainTTL > ttl {
		ttl = chainTTL
	}

	l.mu.Lock()
	l.ttl = ttl
	l.lastReconcile = l.clk.Now()
	l.mu.Unlock()

	// Publish seeded last, after chainBal/ttl are in place, so a goroutine that
	// sees seeded via the lock-free fast path also sees the seeded state.
	l.seeded.Store(true)
	return nil
}

// maintain performs off-critical-path upkeep: a refill if the budget is low and
// a reconcile if one is due (ReconcileInterval since the last). Both sub-steps
// single-flight themselves — the refill via the refilling flag, the reconcile by
// claiming the due slot below — so concurrent Puts don't stack redundant work
// and no dedicated maintenance lock is needed.
func (l *escrowLedger) maintain(ctx context.Context) {
	if err := l.refill(ctx, l.cfg.LowWatermark); err != nil && l.log != nil {
		l.log.Warn("escrow refill failed", "signer", l.signer, "err", err)
	}

	// Claim the reconcile slot by advancing lastReconcile before the query, so
	// concurrent maintainers see "not due" and skip; only one reconciles per
	// interval. On failure the slot stays claimed and the next interval retries.
	l.mu.Lock()
	now := l.clk.Now()
	due := now.Sub(l.lastReconcile) >= l.cfg.ReconcileInterval
	if due {
		l.lastReconcile = now
	}
	l.mu.Unlock()
	if !due {
		return
	}
	if err := l.reconcile(ctx); err != nil && l.log != nil {
		l.log.Warn("escrow reconcile failed", "signer", l.signer, "err", err)
	}
}

// snapshot returns the ledger's current accounting for metrics/diagnostics.
func (l *escrowLedger) snapshot() (chainBal, reserved, available math.Int, inflight int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.chainBal, l.reserved, l.availableLocked(), len(l.inflight)
}
