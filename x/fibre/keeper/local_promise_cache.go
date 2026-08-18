package keeper

import (
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// promiseCacheStaleAfter is how long a signer's cached budget may go
	// unrefreshed, with at least one reservation since the last sweep, before a
	// sweep is forced on the next reservation.
	promiseCacheStaleAfter = time.Hour
	// promiseCacheEvictBuffer is added to the maximum promise lifetime to decide
	// when an idle signer entry may be evicted, i.e. once every promise has had
	// time to either settle or be submitted by the timeout agent.
	promiseCacheEvictBuffer = time.Hour
	// promiseCacheEvictInterval is how often the background eviction loop runs.
	promiseCacheEvictInterval = 10 * time.Minute
)

// promiseStateReader is the read-only committed chain state a sweep needs to
// reconcile cached budgets against the chain. The keeper satisfies it.
type promiseStateReader interface {
	GetEscrowAccount(ctx sdk.Context, signer string) (types.EscrowAccount, bool)
	IsPaymentProcessedByHash(ctx sdk.Context, promiseHash []byte) bool
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
// wall-clock time, goroutines, and map iteration, none of which are permitted in
// state transitions (docs/ai/invariants.md, INV-1); wiring it into a consensus
// path would violate determinism.
type LocalPromiseCache struct {
	reader promiseStateReader

	mu      sync.Mutex
	budgets map[string]*signerBudget
	pending map[string]pendingPromise // key: hex(promiseHash)
}

// signerBudget is the cached budget state for a single escrow account.
type signerBudget struct {
	// remaining is the AvailableBalance from the last sweep minus the sum of
	// amounts reserved for pending promises.
	remaining math.Int
	// lastSweep is when this budget was last reconciled against chain state.
	lastSweep time.Time
	// lastActivity is when this entry was last touched; drives eviction.
	lastActivity time.Time
	// opsSinceSweep counts reservations since the last sweep; drives staleness.
	opsSinceSweep int
	// lastFailSweepH is the block height of the last sweep triggered by a failing
	// reservation; used to rate-limit sweeps for insufficient-balance signers.
	lastFailSweepH int64
	// hashes is the set of hex-encoded promise hashes reserved for this signer.
	hashes map[string]struct{}
}

// pendingPromise is a reservation that has not yet settled on-chain.
type pendingPromise struct {
	signer   string
	blobSize uint32
}

// NewLocalPromiseCache creates an empty cache backed by the given state reader.
func NewLocalPromiseCache(reader promiseStateReader) *LocalPromiseCache {
	return &LocalPromiseCache{
		reader:  reader,
		budgets: make(map[string]*signerBudget),
		pending: make(map[string]pendingPromise),
	}
}

// Reserve records a reservation for an accepted promise against the signer's
// cached budget, returning an error if the remaining budget is insufficient.
// Reservations are idempotent by promise hash.
//
// Callers MUST have already run stateless (signature) and stateful validation.
// The gRPC endpoint is adversarial (INV-2), so signature verification must
// precede any cache mutation to prevent budget poisoning.
func (c *LocalPromiseCache) Reserve(ctx sdk.Context, signer string, promiseHash []byte, blobSize uint32) error {
	key := hex.EncodeToString(promiseHash)
	required := requiredAmount(blobSize)

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.pending[key]; ok {
		return nil // idempotent: already reserved
	}

	b, ok := c.budgets[signer]
	if !ok {
		b = c.sweep(ctx, signer)
	} else if isStale(b) {
		c.sweep(ctx, signer)
	}

	if b.remaining.GTE(required) {
		c.reserve(b, signer, key, blobSize, required)
		return nil
	}

	// Insufficient budget: re-sweep at most once per block for a failing signer to
	// reconcile with any settlements, then retry. This bounds state reads under
	// repeated failing submissions (INV-4).
	if b.lastFailSweepH < ctx.BlockHeight() {
		c.sweep(ctx, signer)
		b.lastFailSweepH = ctx.BlockHeight()
		if b.remaining.GTE(required) {
			c.reserve(b, signer, key, blobSize, required)
			return nil
		}
	}

	return fmt.Errorf("insufficient available balance for signer %s: required %s, remaining %s", signer, required, b.remaining)
}

// reserve commits a reservation to the given budget. Callers must hold c.mu.
func (c *LocalPromiseCache) reserve(b *signerBudget, signer, key string, blobSize uint32, required math.Int) {
	b.remaining = b.remaining.Sub(required)
	b.opsSinceSweep++
	b.lastActivity = time.Now()
	b.hashes[key] = struct{}{}
	c.pending[key] = pendingPromise{signer: signer, blobSize: blobSize}
}

// sweep reconciles a signer's cached budget against committed chain state: it
// re-reads AvailableBalance, drops any locally pending promise already settled
// on-chain, and recomputes the remaining budget. It creates the entry if absent
// and returns it. Callers must hold c.mu.
func (c *LocalPromiseCache) sweep(ctx sdk.Context, signer string) *signerBudget {
	b, ok := c.budgets[signer]
	if !ok {
		b = &signerBudget{hashes: make(map[string]struct{})}
		c.budgets[signer] = b
	}

	available := math.ZeroInt()
	if acc, found := c.reader.GetEscrowAccount(ctx, signer); found {
		available = acc.AvailableBalance.Amount
	}

	committed := math.ZeroInt()
	for key := range b.hashes {
		hash, err := hex.DecodeString(key)
		if err != nil || c.reader.IsPaymentProcessedByHash(ctx, hash) {
			delete(b.hashes, key)
			delete(c.pending, key)
			continue
		}
		committed = committed.Add(requiredAmount(c.pending[key].blobSize))
	}

	remaining := available.Sub(committed)
	if remaining.IsNegative() {
		remaining = math.ZeroInt()
	}
	b.remaining = remaining
	b.lastSweep = time.Now()
	b.lastActivity = time.Now()
	b.opsSinceSweep = 0
	return b
}

// evictLoop periodically removes idle signer entries for the life of the process.
func (c *LocalPromiseCache) evictLoop() {
	ticker := time.NewTicker(promiseCacheEvictInterval)
	defer ticker.Stop()
	for range ticker.C {
		c.evictIdle()
	}
}

// evictIdle removes signer entries with no activity for longer than the maximum
// promise lifetime plus a buffer, by when all of a signer's promises have settled
// or been submitted by the timeout agent. Evicted signers are rebuilt lazily on
// their next validation.
func (c *LocalPromiseCache) evictIdle() {
	threshold := types.MaxPaymentPromiseTimeout + promiseCacheEvictBuffer
	c.mu.Lock()
	defer c.mu.Unlock()
	for signer, b := range c.budgets {
		if time.Since(b.lastActivity) > threshold {
			for key := range b.hashes {
				delete(c.pending, key)
			}
			delete(c.budgets, signer)
		}
	}
}

// isStale reports whether a budget has diverged enough from chain state to force
// a sweep before the next reservation.
func isStale(b *signerBudget) bool {
	return b.opsSinceSweep > 0 && time.Since(b.lastSweep) > promiseCacheStaleAfter
}

// requiredAmount is the escrow amount a promise for the given blob size reserves.
func requiredAmount(blobSize uint32) math.Int {
	return math.NewIntFromUint64(EstimateGasForPayForFibre(blobSize))
}
