package keeper

import (
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// promiseCacheEvictBuffer is added to the maximum settleability window to
	// decide when an idle signer entry may be evicted, i.e. once every promise it
	// could hold has certainly stopped being settleable on-chain.
	promiseCacheEvictBuffer = time.Hour
	// promiseCacheEvictInterval is the minimum time between lazy idle-entry sweeps
	// on the query path.
	promiseCacheEvictInterval = 10 * time.Minute
	// promiseCacheMinSweepAt is the smallest pending count that triggers a
	// memory sweep.
	promiseCacheMinSweepAt = 64
)

// maxPendingPerSigner caps a signer's pending reservations; a var so tests can lower it.
var maxPendingPerSigner = 10_000

// promiseStateReader is the read-only committed chain state a sweep needs to
// reconcile cached budgets against the chain. The keeper satisfies it.
type promiseStateReader interface {
	GetEscrowAccount(ctx sdk.Context, signer string) (types.EscrowAccount, bool)
	IsPromiseProcessed(ctx sdk.Context, creationTimestamp time.Time, promiseHash []byte) bool
	GetParams(ctx sdk.Context) types.Params
}

// LocalPromiseCache is a validator-local, in-memory reservation cache for fibre
// payment promises. It closes the double-spend window between query-time
// validation and on-chain settlement by tracking, per signer, how much of the
// escrow AvailableBalance is already committed to promises this validator has
// accepted but that have not yet settled on-chain.
//
// INVARIANT: this cache must never be reachable from the ABCI/consensus path
// (PrepareProposal, ProcessProposal, FinalizeBlock, message servers). It is used
// only from the ValidatePaymentPromise gRPC query. It intentionally relies on
// wall-clock time and map iteration, neither of which is permitted in state
// transitions; wiring it into a consensus path would violate determinism.
type LocalPromiseCache struct {
	reader promiseStateReader

	// mu guards only budgets and lastEvict; lock order is mu then a signer's mu,
	// and mu is never held while blocking on a signer's mu. Per signer, pending
	// is bounded by maxPendingPerSigner, and idle signers are evicted.
	mu      sync.Mutex
	budgets map[string]*signerBudget
	// lastEvict is when idle entries were last swept; eviction runs lazily on the
	// query path at most once per promiseCacheEvictInterval.
	lastEvict time.Time
}

// signerBudget is the cached budget state for a single escrow account. Its
// budget is available - committed; every field is guarded by mu.
type signerBudget struct {
	mu sync.Mutex
	// pending holds the reservations not yet dropped by a sweep, keyed by
	// hex(promiseHash); committed is the sum of their amounts.
	pending   map[string]pendingPromise
	committed math.Int
	// height, available and balance are the escrow at the newest height read.
	height             int64
	available, balance math.Int
	// sweptBal and sweptAvail are the escrow the last sweep reconciled against;
	// withdrawalDelay is the param it read. Together they gate failure sweeps.
	sweptBal, sweptAvail math.Int
	withdrawalDelay      time.Duration
	// oldestCreation is the earliest creation time in pending.
	oldestCreation time.Time
	// sweepAt is the pending count that triggers a memory sweep.
	sweepAt int
	// lastSweepH is the height of the last sweep; at most one runs per height.
	lastSweepH int64
	// lastActivity is when this entry was last touched; drives eviction.
	lastActivity time.Time
	// evicted is set once the entry is removed from the map; holders must retry.
	evicted bool
}

// pendingPromise is a reservation that has not yet settled on-chain.
type pendingPromise struct {
	blobSize uint32
	// creationTimestamp is the promise's declared creation time. A sweep drops the
	// reservation once creationTimestamp+WithdrawalDelay has passed, since the
	// promise can no longer settle on-chain past its freshness window.
	creationTimestamp time.Time
}

// NewLocalPromiseCache creates an empty cache backed by the given state reader.
func NewLocalPromiseCache(reader promiseStateReader) *LocalPromiseCache {
	return &LocalPromiseCache{
		reader:  reader,
		budgets: make(map[string]*signerBudget),
	}
}

func newSignerBudget() *signerBudget {
	return &signerBudget{
		pending:         make(map[string]pendingPromise),
		committed:       math.ZeroInt(),
		available:       math.ZeroInt(),
		balance:         math.ZeroInt(),
		sweptBal:        math.ZeroInt(),
		sweptAvail:      math.ZeroInt(),
		withdrawalDelay: types.MaxWithdrawalDelay,
		sweepAt:         promiseCacheMinSweepAt,
		lastActivity:    time.Now(),
	}
}

// Reserve records a reservation for an accepted promise against the signer's
// cached budget, returning an error if the budget is insufficient or the signer
// is at its pending cap. Reservations are idempotent by promise hash.
//
// Callers MUST have already run stateless (signature) and stateful validation.
// The gRPC endpoint is adversarial, so signature verification must precede any
// cache mutation to prevent budget poisoning.
func (c *LocalPromiseCache) Reserve(ctx sdk.Context, signer string, promiseHash []byte, blobSize uint32, creationTimestamp time.Time) error {
	key := hex.EncodeToString(promiseHash)
	required := requiredAmount(blobSize)

	// Read the escrow before taking any lock so the store read does not
	// serialize other requests.
	acc, found := c.reader.GetEscrowAccount(ctx, signer)
	available, balance := math.ZeroInt(), math.ZeroInt()
	if found {
		available, balance = acc.AvailableBalance.Amount, acc.Balance.Amount
	}

	b := c.lockSigner(signer)
	defer b.mu.Unlock()
	b.lastActivity = time.Now()

	// Keep the escrow of the newest height seen, so a read from an older height
	// cannot raise the budget above what a sweep at a newer height freed.
	if h := ctx.BlockHeight(); h >= b.height {
		b.height, b.available, b.balance = h, available, balance
	}

	if _, ok := b.pending[key]; ok {
		return nil // idempotent: already reserved
	}

	// Bound memory for signers that never fail: sweep once pending doubles.
	if len(b.pending) >= b.sweepAt {
		c.sweep(ctx, b)
	}
	if b.tryAdmit(key, blobSize, required, creationTimestamp) {
		return nil
	}

	// Sweep on failure only if something on-chain could have freed an entry.
	if b.sweepDue(ctx) && c.sweep(ctx, b) && b.tryAdmit(key, blobSize, required, creationTimestamp) {
		return nil
	}

	if len(b.pending) >= maxPendingPerSigner {
		return fmt.Errorf("too many pending promises for signer %s: cap %d reached", signer, maxPendingPerSigner)
	}
	return fmt.Errorf("insufficient available balance for signer %s: required %s, available %s, committed %s", signer, required, b.available, b.committed)
}

// lockSigner returns the signer's entry with its mu held, creating it if absent.
func (c *LocalPromiseCache) lockSigner(signer string) *signerBudget {
	for {
		c.mu.Lock()
		// Reclaim idle entries lazily on the query path, at most once per interval.
		if time.Since(c.lastEvict) > promiseCacheEvictInterval {
			c.evictIdleLocked()
			c.lastEvict = time.Now()
		}
		b, ok := c.budgets[signer]
		if !ok {
			b = newSignerBudget()
			c.budgets[signer] = b
		}
		c.mu.Unlock()

		b.mu.Lock()
		if !b.evicted {
			return b
		}
		b.mu.Unlock() // evicted between lookup and lock: look up again
	}
}

// tryAdmit reserves the promise if the signer is under its cap and has budget.
// The budget uses AvailableBalance, not the Balance the chain settles from, so
// the cache is stricter than the chain. Callers must hold b.mu.
func (b *signerBudget) tryAdmit(key string, blobSize uint32, required math.Int, creationTimestamp time.Time) bool {
	if len(b.pending) >= maxPendingPerSigner || b.available.Sub(b.committed).LT(required) {
		return false
	}
	if len(b.pending) == 0 || creationTimestamp.Before(b.oldestCreation) {
		b.oldestCreation = creationTimestamp
	}
	b.pending[key] = pendingPromise{blobSize: blobSize, creationTimestamp: creationTimestamp}
	b.committed = b.committed.Add(required)
	return true
}

// sweepDue reports whether a sweep could free an entry: the escrow changed since
// the last sweep, or the oldest pending promise has passed its settleability window.
func (b *signerBudget) sweepDue(ctx sdk.Context) bool {
	return !b.balance.Equal(b.sweptBal) || !b.available.Equal(b.sweptAvail) ||
		(len(b.pending) > 0 && !b.oldestCreation.After(ctx.BlockTime().Add(-b.withdrawalDelay)))
}

// sweep drops pending promises that settled on-chain or can no longer settle,
// and recomputes committed. It runs at most once per height and only from a ctx
// at the entry's newest height, and reports whether it ran. Callers must hold b.mu.
func (c *LocalPromiseCache) sweep(ctx sdk.Context, b *signerBudget) bool {
	if ctx.BlockHeight() != b.height || b.lastSweepH >= b.height {
		return false
	}

	// A promise stops being settleable on-chain once its creation_timestamp falls
	// before current_time - WithdrawalDelay; the on-chain freshness floor only
	// shortens this window, so dropping it here is conservative.
	params := c.reader.GetParams(ctx)
	staleBefore := ctx.BlockTime().Add(-params.WithdrawalDelay)

	committed := math.ZeroInt()
	var oldest time.Time
	for key, p := range b.pending {
		hash, err := hex.DecodeString(key)
		if err != nil || !p.creationTimestamp.After(staleBefore) || c.reader.IsPromiseProcessed(ctx, p.creationTimestamp, hash) {
			delete(b.pending, key)
			continue
		}
		committed = committed.Add(requiredAmount(p.blobSize))
		if oldest.IsZero() || p.creationTimestamp.Before(oldest) {
			oldest = p.creationTimestamp
		}
	}

	b.committed, b.oldestCreation = committed, oldest
	b.sweptBal, b.sweptAvail, b.withdrawalDelay = b.balance, b.available, params.WithdrawalDelay
	b.sweepAt = max(2*len(b.pending), promiseCacheMinSweepAt)
	b.lastSweepH = b.height
	return true
}

// evictIdleLocked removes signer entries with no activity for longer than the
// maximum settleability window (MaxWithdrawalDelay) plus a buffer, by when any
// promise the entry could hold has certainly stopped being settleable on-chain.
// Bounding on MaxWithdrawalDelay (not MaxPaymentPromiseTimeout) is required: a
// promise stays settleable via the timeout path until creation_timestamp+
// WithdrawalDelay, so a shorter threshold could evict a still-live reservation and
// reopen the double-spend window. Evicted signers are rebuilt lazily on their next
// validation. Callers must hold c.mu.
func (c *LocalPromiseCache) evictIdleLocked() {
	threshold := types.MaxWithdrawalDelay + promiseCacheEvictBuffer
	for signer, b := range c.budgets {
		if !b.mu.TryLock() {
			continue // busy means active; never block on b.mu while holding c.mu
		}
		if time.Since(b.lastActivity) > threshold {
			b.evicted = true
			delete(c.budgets, signer)
		}
		b.mu.Unlock()
	}
}

// requiredAmount is the escrow amount a promise for the given blob size reserves.
func requiredAmount(blobSize uint32) math.Int {
	return types.PaymentAmount(blobSize).Amount
}
