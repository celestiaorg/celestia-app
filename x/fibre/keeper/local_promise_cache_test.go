package keeper

import (
	"encoding/hex"
	"sync/atomic"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// fakeStateReader is an in-memory promiseStateReader for cache tests. It ignores
// the sdk.Context; the cache only uses it for block height.
type fakeStateReader struct {
	available       map[string]math.Int // signer -> AvailableBalance amount
	processed       map[string]bool     // hex(hash) -> processed on-chain
	escrowGets      atomic.Int64        // counts GetEscrowAccount calls
	withdrawalDelay time.Duration       // params.WithdrawalDelay; defaults if zero
}

func (f *fakeStateReader) GetEscrowAccount(_ sdk.Context, signer string) (types.EscrowAccount, bool) {
	f.escrowGets.Add(1)
	amt, ok := f.available[signer]
	if !ok {
		return types.EscrowAccount{}, false
	}
	return types.EscrowAccount{
		Signer:           signer,
		AvailableBalance: sdk.NewCoin(appconsts.BondDenom, amt),
	}, true
}

func (f *fakeStateReader) IsPaymentProcessedByHash(_ sdk.Context, hash []byte) bool {
	return f.processed[hex.EncodeToString(hash)]
}

func (f *fakeStateReader) GetParams(_ sdk.Context) types.Params {
	delay := f.withdrawalDelay
	if delay == 0 {
		delay = types.DefaultWithdrawalDelay
	}
	return types.Params{WithdrawalDelay: delay}
}

// gas for a zero-size blob; used to size test balances.
var zeroBlobGas = math.NewIntFromUint64(EstimateGasForPayForFibre(0))

func ctxAtHeight(h int64) sdk.Context { return sdk.Context{}.WithBlockHeight(h) }

// promiseTS is a fixed, fresh promise creation time. The test contexts use a zero
// block time, so any real timestamp stays within the settleability window.
var promiseTS = time.Unix(1_000_000, 0).UTC()

func TestReserveWithinBudget(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": zeroBlobGas.MulRaw(2)}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))
	require.Equal(t, zeroBlobGas, c.budgets["a"].remaining)
}

func TestReserveDoubleSpendRejected(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": zeroBlobGas}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))
	err := c.Reserve(ctxAtHeight(1), "a", []byte{0x02}, 0, promiseTS)
	require.ErrorContains(t, err, "insufficient available balance")
	require.True(t, c.budgets["a"].remaining.IsZero())
}

func TestReserveIdempotent(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": zeroBlobGas.MulRaw(2)}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))
	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))
	// Budget is decremented once despite two identical submissions.
	require.Equal(t, zeroBlobGas, c.budgets["a"].remaining)
}

func TestSweepDropsProcessed(t *testing.T) {
	r := &fakeStateReader{
		available: map[string]math.Int{"a": zeroBlobGas.MulRaw(2)},
		processed: map[string]bool{},
	}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))
	require.Equal(t, zeroBlobGas, c.budgets["a"].remaining)

	// The promise settles on-chain; a sweep should drop it and free its budget.
	r.processed[hex.EncodeToString([]byte{0x01})] = true
	c.mu.Lock()
	c.sweep(ctxAtHeight(1), "a", c.availableBalance(ctxAtHeight(1), "a"))
	c.mu.Unlock()

	require.Equal(t, zeroBlobGas.MulRaw(2), c.budgets["a"].remaining)
	require.Empty(t, c.pending)
}

func TestFailingSweepsRateLimited(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": math.ZeroInt()}}
	c := NewLocalPromiseCache(r)

	// Each reservation reads the escrow account exactly once; the retry sweep
	// reuses that read.
	require.Error(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))
	require.Equal(t, int64(1), r.escrowGets.Load())
	require.Equal(t, int64(1), c.budgets["a"].lastFailSweepH)

	// Second failing reservation in the same block: no retry sweep.
	before := c.budgets["a"].lastSweep
	require.Error(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x02}, 0, promiseTS))
	require.Equal(t, int64(2), r.escrowGets.Load())
	require.Equal(t, before, c.budgets["a"].lastSweep)

	// New block: one more retry sweep is allowed.
	require.Error(t, c.Reserve(ctxAtHeight(2), "a", []byte{0x03}, 0, promiseTS))
	require.Equal(t, int64(3), r.escrowGets.Load())
	require.Equal(t, int64(2), c.budgets["a"].lastFailSweepH)
}

func TestConcurrentReserveNoOversubscribe(t *testing.T) {
	const budgetUnits = 5
	const promises = 50
	r := &fakeStateReader{available: map[string]math.Int{"a": zeroBlobGas.MulRaw(budgetUnits)}}
	c := NewLocalPromiseCache(r)

	results := make(chan error, promises)
	for i := range promises {
		go func(i int) {
			results <- c.Reserve(ctxAtHeight(1), "a", []byte{byte(i)}, 0, promiseTS)
		}(i)
	}

	ok := 0
	for range promises {
		if <-results == nil {
			ok++
		}
	}
	// Exactly budgetUnits reservations may succeed; the rest must be rejected.
	require.Equal(t, budgetUnits, ok)
	require.True(t, c.budgets["a"].remaining.IsZero())
}

func TestEvictIdle(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": zeroBlobGas.MulRaw(2)}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))

	// Backdate activity beyond the eviction threshold.
	c.budgets["a"].lastActivity = time.Now().Add(-(types.MaxWithdrawalDelay + 2*time.Hour))
	c.evictIdleLocked()

	require.Empty(t, c.budgets)
	require.Empty(t, c.pending)
}

// TestSweepDropsExpired verifies that a sweep frees a reservation once the promise
// can no longer settle on-chain (past creation_timestamp+WithdrawalDelay), even
// though it never settled. This prevents an abandoned promise from permanently
// shrinking a signer's cached budget.
func TestSweepDropsExpired(t *testing.T) {
	r := &fakeStateReader{
		available:       map[string]math.Int{"a": zeroBlobGas.MulRaw(2)},
		withdrawalDelay: 24 * time.Hour,
	}
	c := NewLocalPromiseCache(r)

	created := time.Unix(1_000_000, 0).UTC()
	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, created))
	require.Equal(t, zeroBlobGas, c.budgets["a"].remaining)

	// A block whose time is past the promise's settleability window; the reservation
	// can never settle, so the sweep must drop it and restore the full budget.
	ctx := ctxAtHeight(2).WithBlockTime(created.Add(25 * time.Hour))
	c.mu.Lock()
	c.sweep(ctx, "a", c.availableBalance(ctx, "a"))
	c.mu.Unlock()

	require.Equal(t, zeroBlobGas.MulRaw(2), c.budgets["a"].remaining)
	require.Empty(t, c.pending)
}

// TestReserveResweepsWhenAvailableDrops verifies that a drop in on-chain
// AvailableBalance since the last sweep (for example a withdrawal request) forces
// a sweep before the next reservation, even inside the staleness window, so
// promises created after the drop cannot be admitted against the old budget.
func TestReserveResweepsWhenAvailableDrops(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": zeroBlobGas.MulRaw(3)}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))
	require.Equal(t, zeroBlobGas.MulRaw(2), c.budgets["a"].remaining)

	// The signer requests a withdrawal of two units: AvailableBalance drops to one
	// unit, which is already committed to promise 0x01.
	r.available["a"] = zeroBlobGas
	require.False(t, isStale(c.budgets["a"]))

	err := c.Reserve(ctxAtHeight(2), "a", []byte{0x02}, 0, promiseTS)
	require.ErrorContains(t, err, "insufficient available balance")
	require.True(t, c.budgets["a"].remaining.IsZero())
	require.NotContains(t, c.pending, hex.EncodeToString([]byte{0x02}))
}

// TestReserveDepositDoesNotResweep verifies that an increase in AvailableBalance
// does not force a sweep: the cached budget is then only stricter than the chain.
func TestReserveDepositDoesNotResweep(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": zeroBlobGas.MulRaw(2)}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0, promiseTS))
	before := c.budgets["a"].lastSweep

	r.available["a"] = zeroBlobGas.MulRaw(5)
	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x02}, 0, promiseTS))
	require.Equal(t, before, c.budgets["a"].lastSweep)
	require.True(t, c.budgets["a"].remaining.IsZero())
}
