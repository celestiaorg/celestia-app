package keeper

import (
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// fakeStateReader is an in-memory promiseStateReader for cache tests. Its maps
// must not be mutated while Reserves run concurrently.
type fakeStateReader struct {
	available   map[string]math.Int           // signer -> AvailableBalance amount
	balance     map[string]math.Int           // signer -> Balance amount; defaults to available
	availableAt map[int64]map[string]math.Int // height -> signer -> AvailableBalance override
	processed   map[string]int64              // hex(hash) -> height it settled at

	escrowGets  atomic.Int64 // counts GetEscrowAccount calls
	lookups     atomic.Int64 // counts IsPromiseProcessed calls
	paramsReads atomic.Int64 // counts GetParams calls, i.e. sweeps

	withdrawalDelay time.Duration     // params.WithdrawalDelay; defaults if zero
	onLookup        func(hash []byte) // called on every IsPromiseProcessed
}

func (f *fakeStateReader) GetEscrowAccount(ctx sdk.Context, signer string) (types.EscrowAccount, bool) {
	f.escrowGets.Add(1)
	amt, ok := f.available[signer]
	if byH, hok := f.availableAt[ctx.BlockHeight()]; hok {
		amt, ok = byH[signer]
	}
	if !ok {
		return types.EscrowAccount{}, false
	}
	bal, bok := f.balance[signer]
	if !bok {
		bal = amt
	}
	return types.EscrowAccount{
		Signer:           signer,
		Balance:          sdk.NewCoin(appconsts.BondDenom, bal),
		AvailableBalance: sdk.NewCoin(appconsts.BondDenom, amt),
	}, true
}

func (f *fakeStateReader) IsPromiseProcessed(ctx sdk.Context, _ time.Time, hash []byte) bool {
	f.lookups.Add(1)
	if f.onLookup != nil {
		f.onLookup(hash)
	}
	h, ok := f.processed[hex.EncodeToString(hash)]
	return ok && ctx.BlockHeight() >= h
}

func (f *fakeStateReader) GetParams(_ sdk.Context) types.Params {
	f.paramsReads.Add(1)
	delay := f.withdrawalDelay
	if delay == 0 {
		delay = types.DefaultWithdrawalDelay
	}
	return types.Params{WithdrawalDelay: delay}
}

// settle marks a promise as processed on-chain from height h on.
func (f *fakeStateReader) settle(hash []byte, h int64) {
	if f.processed == nil {
		f.processed = map[string]int64{}
	}
	f.processed[hex.EncodeToString(hash)] = h
}

// Payment for a zero-size blob; used to size test balances.
var zeroBlobPayment = types.PaymentAmount(0).Amount

func units(n int64) math.Int { return zeroBlobPayment.MulRaw(n) }

func ctxAtHeight(h int64) sdk.Context { return sdk.Context{}.WithBlockHeight(h) }

// promiseTS is a fixed, fresh promise creation time. The test contexts use a zero
// block time, so any real timestamp stays within the settleability window.
var promiseTS = time.Unix(1_000_000, 0).UTC()

// headroom is the signer's budget, available - committed.
func headroom(c *LocalPromiseCache, signer string) math.Int {
	b := c.budgets[signer]
	return b.available.Sub(b.committed)
}

func hashN(i int) []byte { return []byte(fmt.Sprintf("h%06d", i)) }

func TestReserveWithinBudget(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": units(2)}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))
	require.Equal(t, units(1), headroom(c, "a"))
	require.Equal(t, units(1), c.budgets["a"].committed)
}

func TestReserveDoubleSpendRejected(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": units(1)}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))
	err := c.Reserve(ctxAtHeight(1), "a", []byte{0x02}, 0, promiseTS)
	require.ErrorContains(t, err, "insufficient available balance")
	require.True(t, headroom(c, "a").IsZero())
}

func TestReserveIdempotent(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": units(2)}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))
	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))
	// Budget is decremented once despite two identical submissions.
	require.Equal(t, units(1), headroom(c, "a"))
	require.Len(t, c.budgets["a"].pending, 1)
}

func TestSweepDropsProcessed(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": units(2)}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))

	// The promise settles on-chain; a sweep should drop it and free its budget.
	r.settle([]byte{0x01}, 1)
	b := c.budgets["a"]
	b.mu.Lock()
	require.True(t, c.sweep(ctxAtHeight(1), b))
	b.mu.Unlock()

	require.Equal(t, units(2), headroom(c, "a"))
	require.Empty(t, b.pending)
}

// TestSweepDropsExpired verifies that a sweep frees a reservation once the promise
// can no longer settle on-chain (past creation_timestamp+WithdrawalDelay), even
// though it never settled.
func TestSweepDropsExpired(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": units(1)}, withdrawalDelay: 24 * time.Hour}
	c := NewLocalPromiseCache(r)

	created := time.Unix(1_000_000, 0).UTC()
	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, created))

	// Past the promise's settleability window the failing reservation sweeps,
	// drops the expired one and takes its budget.
	ctx := ctxAtHeight(2).WithBlockTime(created.Add(25 * time.Hour))
	require.NoError(t, c.Reserve(ctx, "a", []byte{0x02}, 0, created.Add(25*time.Hour)))
	require.Len(t, c.budgets["a"].pending, 1)
	require.Contains(t, c.budgets["a"].pending, hex.EncodeToString([]byte{0x02}))
	require.Equal(t, units(1), c.budgets["a"].committed)
}

// TestFailingSweepsRateLimited verifies that a failing reservation sweeps at
// most once per height, and only when the escrow changed since the last sweep
// or the oldest pending promise expired.
func TestFailingSweepsRateLimited(t *testing.T) {
	const delay = 24 * time.Hour
	r := &fakeStateReader{available: map[string]math.Int{"a": units(1)}, withdrawalDelay: delay}
	c := NewLocalPromiseCache(r)
	sweeps := func() int64 { return r.paramsReads.Load() }
	reserve := func(ctx sdk.Context, h byte) error {
		return c.Reserve(ctx, "a", []byte{h}, 0, promiseTS)
	}

	require.NoError(t, reserve(ctxAtHeight(1), 0x01))
	// First failure: the escrow was never swept, so it sweeps once.
	require.Error(t, reserve(ctxAtHeight(1), 0x02))
	require.Equal(t, int64(1), sweeps())
	require.Equal(t, int64(1), r.lookups.Load())
	// Same height: no second sweep.
	require.Error(t, reserve(ctxAtHeight(1), 0x03))
	require.Equal(t, int64(1), sweeps())

	// New height, unchanged escrow, nothing expired: no sweep.
	require.Error(t, reserve(ctxAtHeight(2), 0x04))
	require.Equal(t, int64(1), sweeps())

	// Balance changed (AvailableBalance unchanged): sweep, once per height.
	r.balance = map[string]math.Int{"a": units(2)}
	require.Error(t, reserve(ctxAtHeight(3), 0x05))
	require.Equal(t, int64(2), sweeps())
	require.Error(t, reserve(ctxAtHeight(3), 0x06))
	require.Equal(t, int64(2), sweeps())

	// AvailableBalance changed: sweep.
	r.available["a"] = units(1).AddRaw(1)
	require.Error(t, reserve(ctxAtHeight(4), 0x07))
	require.Equal(t, int64(3), sweeps())

	// Unchanged again: no sweep.
	require.Error(t, reserve(ctxAtHeight(5), 0x08))
	require.Equal(t, int64(3), sweeps())

	// The oldest pending promise expires: sweep, which frees the budget.
	require.NoError(t, c.Reserve(ctxAtHeight(6).WithBlockTime(promiseTS.Add(delay)), "a", []byte{0x09}, 0, promiseTS.Add(delay)))
	require.Equal(t, int64(4), sweeps())
	require.Equal(t, int64(9), r.escrowGets.Load()) // one escrow read per Reserve
}

// TestTopUpHiddenSettlementSweepsAfterInterval verifies that a signer whose
// settlements were hidden by an exact escrow top-up recovers within
// promiseCacheSweepEvery heights.
func TestTopUpHiddenSettlementSweepsAfterInterval(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": units(2)}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", hashN(1), 0, promiseTS))
	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", hashN(2), 0, promiseTS))
	require.Error(t, c.Reserve(ctxAtHeight(1), "a", hashN(3), 0, promiseTS)) // sweeps, nothing settled

	// Both settle at height 2 and the signer tops the escrow back to units(2).
	r.settle(hashN(1), 2)
	r.settle(hashN(2), 2)
	require.Error(t, c.Reserve(ctxAtHeight(2), "a", hashN(4), 0, promiseTS))
	require.Error(t, c.Reserve(ctxAtHeight(promiseCacheSweepEvery), "a", hashN(5), 0, promiseTS))

	require.NoError(t, c.Reserve(ctxAtHeight(1+promiseCacheSweepEvery), "a", hashN(6), 0, promiseTS))
	require.Equal(t, units(1), c.budgets["a"].committed)
}

// TestAvailableDropLowersBudgetWithoutSweep verifies that a withdrawal request
// lowers the budget at once, with no sweep and no processed lookups.
func TestAvailableDropLowersBudgetWithoutSweep(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": units(3)}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))

	// AvailableBalance drops by one unit: one unit of budget is left.
	r.available["a"] = units(2)
	require.NoError(t, c.Reserve(ctxAtHeight(2), "a", []byte{0x02}, 0, promiseTS))
	require.Zero(t, r.lookups.Load())
	require.Zero(t, r.paramsReads.Load())
	require.True(t, headroom(c, "a").IsZero())

	err := c.Reserve(ctxAtHeight(2), "a", []byte{0x03}, 0, promiseTS)
	require.ErrorContains(t, err, "insufficient available balance")
	require.NotContains(t, c.budgets["a"].pending, hex.EncodeToString([]byte{0x03}))
}

// TestReserveDepositDoesNotResweep verifies that a deposit raises the budget
// with no sweep.
func TestReserveDepositDoesNotResweep(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": units(1)}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))

	r.available["a"] = units(5)
	require.NoError(t, c.Reserve(ctxAtHeight(2), "a", []byte{0x02}, 0, promiseTS))
	require.Zero(t, r.paramsReads.Load())
	require.Zero(t, r.lookups.Load())
	require.Equal(t, units(3), headroom(c, "a"))
}

// TestStaleHeightReadCannotRaiseBudget replays a request that read the escrow
// at h1 but reaches the cache after a sweep at h2 dropped a promise that
// settled at h2; it must not count that promise's amount as free twice.
func TestStaleHeightReadCannotRaiseBudget(t *testing.T) {
	r := &fakeStateReader{availableAt: map[int64]map[string]math.Int{
		1: {"a": units(2)},
		2: {"a": units(1)}, // P settled at h2 and paid one unit
	}}
	c := NewLocalPromiseCache(r)

	p := []byte{0x01}
	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", p, 0, promiseTS))
	r.settle(p, 2)

	// R2 at h2: P is still committed, so it fails, sweeps P away and admits Q.
	require.NoError(t, c.Reserve(ctxAtHeight(2), "a", []byte{0x02}, 0, promiseTS))
	require.NotContains(t, c.budgets["a"].pending, hex.EncodeToString(p))

	// R1 at h1 reads two units; the chain has one, already committed to Q.
	err := c.Reserve(ctxAtHeight(1), "a", []byte{0x03}, 0, promiseTS)
	require.ErrorContains(t, err, "insufficient available balance")
	require.Equal(t, int64(2), c.budgets["a"].height)
	require.Equal(t, units(1), c.budgets["a"].available)
}

// TestOldHeightCtxDoesNotSweep verifies that neither trigger sweeps from a ctx
// below the entry's newest height.
func TestOldHeightCtxDoesNotSweep(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": units(1), "b": units(10)}}
	c := NewLocalPromiseCache(r)

	// Failure trigger: a full budget at h2, then a failing reservation at h1.
	require.NoError(t, c.Reserve(ctxAtHeight(2), "a", []byte{0x01}, 0, promiseTS))
	require.Error(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x02}, 0, promiseTS))

	// Memory trigger: pending at sweepAt at h2, then a reservation at h1.
	require.NoError(t, c.Reserve(ctxAtHeight(2), "b", []byte{0x01}, 0, promiseTS))
	require.NoError(t, c.Reserve(ctxAtHeight(2), "b", []byte{0x02}, 0, promiseTS))
	c.budgets["b"].sweepAt = 2
	require.NoError(t, c.Reserve(ctxAtHeight(1), "b", []byte{0x03}, 0, promiseTS))

	require.Zero(t, r.lookups.Load())
	require.Zero(t, r.paramsReads.Load())
}

// TestPendingCapPerSigner verifies the per-signer cap: it rejects, a sweep that
// frees entries lets the signer admit again, and other signers are unaffected.
func TestPendingCapPerSigner(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": units(1_000_000), "b": units(1)}}
	c := NewLocalPromiseCache(r)

	for i := range maxPendingPerSigner {
		require.NoError(t, c.Reserve(ctxAtHeight(1), "a", hashN(i), 0, promiseTS))
	}
	err := c.Reserve(ctxAtHeight(1), "a", hashN(maxPendingPerSigner), 0, promiseTS)
	require.ErrorContains(t, err, "too many pending promises")
	require.Len(t, c.budgets["a"].pending, maxPendingPerSigner)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "b", []byte{0x01}, 0, promiseTS))

	// Two promises settle at h2, changing the escrow: the signer admits again.
	r.settle(hashN(0), 2)
	r.settle(hashN(1), 2)
	r.available["a"] = units(1_000_000 - 2)
	require.NoError(t, c.Reserve(ctxAtHeight(2), "a", hashN(maxPendingPerSigner), 0, promiseTS))
	require.Len(t, c.budgets["a"].pending, maxPendingPerSigner-1)
}

// TestMemoryTriggerTrimsSettled verifies that a funded signer that never fails
// still drops settled entries once pending reaches sweepAt.
func TestMemoryTriggerTrimsSettled(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": units(1_000)}}
	c := NewLocalPromiseCache(r)

	for i := range promiseCacheMinSweepAt {
		require.NoError(t, c.Reserve(ctxAtHeight(1), "a", hashN(i), 0, promiseTS))
		r.settle(hashN(i), 2)
	}
	require.Zero(t, r.paramsReads.Load())

	require.NoError(t, c.Reserve(ctxAtHeight(2), "a", hashN(promiseCacheMinSweepAt), 0, promiseTS))
	require.Equal(t, int64(1), r.paramsReads.Load())
	require.Len(t, c.budgets["a"].pending, 1)
	require.Equal(t, units(1), c.budgets["a"].committed)
	require.Equal(t, promiseCacheMinSweepAt, c.budgets["a"].sweepAt)
}

func TestEvictIdle(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": units(2)}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))
	b := c.budgets["a"]

	// Backdate activity beyond the eviction threshold.
	b.lastActivity = time.Now().Add(-(types.MaxWithdrawalDelay + 2*time.Hour))
	c.mu.Lock()
	c.evictIdleLocked()
	c.mu.Unlock()

	require.Empty(t, c.budgets)
	require.True(t, b.evicted)
}

// TestEvictionRaceKeepsCommitted verifies that eviction skips a busy entry, and
// that a reservation waiting on an entry evicted under it lands in the live
// entry rather than the detached one.
func TestEvictionRaceKeepsCommitted(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": units(1), "b": units(1)}}
	c := NewLocalPromiseCache(r)
	idle := time.Now().Add(-(types.MaxWithdrawalDelay + 2*time.Hour))

	// A busy entry survives eviction and keeps its committed amount.
	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))
	a := c.budgets["a"]
	a.mu.Lock()
	a.lastActivity = idle
	c.mu.Lock()
	c.evictIdleLocked()
	c.mu.Unlock()
	a.mu.Unlock()
	require.Same(t, a, c.budgets["a"])
	require.ErrorContains(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x02}, 0, promiseTS), "insufficient available balance")

	// A Reserve blocked on b.mu while the evictor removes b must retry.
	c.mu.Lock()
	b := newSignerBudget()
	c.budgets["b"] = b
	c.mu.Unlock()
	b.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- c.Reserve(ctxAtHeight(1), "b", []byte{0x01}, 0, promiseTS) }()
	time.Sleep(50 * time.Millisecond) // let it look up b and block on b.mu
	c.mu.Lock()
	b.evicted = true // what evictIdleLocked does after TryLock
	delete(c.budgets, "b")
	c.mu.Unlock()
	b.mu.Unlock()
	require.NoError(t, <-done)

	require.Empty(t, b.pending)
	live := c.budgets["b"]
	require.NotSame(t, b, live)
	require.Len(t, live.pending, 1)
	require.ErrorContains(t, c.Reserve(ctxAtHeight(1), "b", []byte{0x02}, 0, promiseTS), "insufficient available balance")
}

// TestSignerSweepDoesNotBlockOthers verifies that a sweep of one signer does
// not block another signer's reservation.
func TestSignerSweepDoesNotBlockOthers(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": units(1), "b": units(1)}}
	c := NewLocalPromiseCache(r)
	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0xa1}, 0, promiseTS))

	entered, release := make(chan struct{}), make(chan struct{})
	r.onLookup = func(hash []byte) {
		if hash[0] == 0xa1 {
			close(entered)
			<-release
		}
	}
	// A's failing reservation sweeps and blocks inside the processed lookup.
	aDone := make(chan error, 1)
	go func() { aDone <- c.Reserve(ctxAtHeight(1), "a", []byte{0xa2}, 0, promiseTS) }()
	<-entered

	bDone := make(chan error, 1)
	go func() { bDone <- c.Reserve(ctxAtHeight(1), "b", []byte{0xb1}, 0, promiseTS) }()
	select {
	case err := <-bDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("signer b blocked behind signer a's sweep")
	}
	close(release)
	require.Error(t, <-aDone)
}

// TestConcurrentReserveNoOversubscribe races reservations across many signers
// with concurrent eviction of idle signers, some of which are reserved again.
func TestConcurrentReserveNoOversubscribe(t *testing.T) {
	const (
		budgetUnits = 5
		signers     = 16
		perSigner   = 40
	)
	r := &fakeStateReader{available: map[string]math.Int{}}
	c := NewLocalPromiseCache(r)
	idle := time.Now().Add(-(types.MaxWithdrawalDelay + 2*time.Hour))
	name := func(i int) string { return fmt.Sprintf("s%02d", i) }
	for i := range signers {
		r.available[name(i)] = units(budgetUnits)
		// Odd signers start as idle entries the evictor races to remove.
		if i%2 == 1 {
			b := newSignerBudget()
			b.lastActivity = idle
			c.budgets[name(i)] = b
		}
	}

	var (
		wg   sync.WaitGroup
		stop atomic.Bool
		ok   [signers]atomic.Int64
	)
	start, evictorDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(evictorDone)
		<-start
		for !stop.Load() {
			c.mu.Lock()
			c.evictIdleLocked()
			c.mu.Unlock()
		}
	}()
	for s := range signers {
		for i := range perSigner {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				h := []byte{byte(s), byte(i)}
				if c.Reserve(ctxAtHeight(1+int64(i%3)), name(s), h, 0, promiseTS) == nil {
					ok[s].Add(1)
				}
			}()
		}
	}
	close(start)
	wg.Wait()
	stop.Store(true)
	<-evictorDone

	for s := range signers {
		require.Equal(t, int64(budgetUnits), ok[s].Load(), name(s))
		b := c.budgets[name(s)]
		require.Len(t, b.pending, budgetUnits, name(s))
		require.Equal(t, units(budgetUnits), b.committed, name(s))
	}
}
