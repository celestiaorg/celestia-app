package keeper

import (
	"encoding/hex"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// fakeStateReader is an in-memory promiseStateReader for cache tests. It ignores
// the sdk.Context; the cache only uses it for block height.
type fakeStateReader struct {
	available  map[string]math.Int // signer -> AvailableBalance amount
	processed  map[string]bool     // hex(hash) -> processed on-chain
	escrowGets int                 // counts GetEscrowAccount calls (sweeps)
}

func (f *fakeStateReader) GetEscrowAccount(_ sdk.Context, signer string) (types.EscrowAccount, bool) {
	f.escrowGets++
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

// gas for a zero-size blob; used to size test balances.
var zeroBlobGas = math.NewIntFromUint64(EstimateGasForPayForFibre(0))

func ctxAtHeight(h int64) sdk.Context { return sdk.Context{}.WithBlockHeight(h) }

func TestReserveWithinBudget(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": zeroBlobGas.MulRaw(2)}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0))
	require.Equal(t, zeroBlobGas, c.budgets["a"].remaining)
}

func TestReserveDoubleSpendRejected(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": zeroBlobGas}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0))
	err := c.Reserve(ctxAtHeight(1), "a", []byte{0x02}, 0)
	require.ErrorContains(t, err, "insufficient available balance")
	require.True(t, c.budgets["a"].remaining.IsZero())
}

func TestReserveIdempotent(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": zeroBlobGas.MulRaw(2)}}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0))
	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0))
	// Budget is decremented once despite two identical submissions.
	require.Equal(t, zeroBlobGas, c.budgets["a"].remaining)
}

func TestSweepDropsProcessed(t *testing.T) {
	r := &fakeStateReader{
		available: map[string]math.Int{"a": zeroBlobGas.MulRaw(2)},
		processed: map[string]bool{},
	}
	c := NewLocalPromiseCache(r)

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0))
	require.Equal(t, zeroBlobGas, c.budgets["a"].remaining)

	// The promise settles on-chain; a sweep should drop it and free its budget.
	r.processed[hex.EncodeToString([]byte{0x01})] = true
	c.mu.Lock()
	c.sweep(ctxAtHeight(1), "a")
	c.mu.Unlock()

	require.Equal(t, zeroBlobGas.MulRaw(2), c.budgets["a"].remaining)
	require.Empty(t, c.pending)
}

func TestFailingSweepsRateLimited(t *testing.T) {
	r := &fakeStateReader{available: map[string]math.Int{"a": math.ZeroInt()}}
	c := NewLocalPromiseCache(r)

	// First failing reservation at height 1: initial sweep + one rate-limited
	// retry sweep = 2 reads.
	require.Error(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0))
	require.Equal(t, 2, r.escrowGets)

	// Second failing reservation in the same block: no further sweep.
	require.Error(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x02}, 0))
	require.Equal(t, 2, r.escrowGets)

	// New block: one more retry sweep is allowed.
	require.Error(t, c.Reserve(ctxAtHeight(2), "a", []byte{0x03}, 0))
	require.Equal(t, 3, r.escrowGets)
}

func TestConcurrentReserveNoOversubscribe(t *testing.T) {
	const budgetUnits = 5
	const promises = 50
	r := &fakeStateReader{available: map[string]math.Int{"a": zeroBlobGas.MulRaw(budgetUnits)}}
	c := NewLocalPromiseCache(r)

	results := make(chan error, promises)
	for i := range promises {
		go func(i int) {
			results <- c.Reserve(ctxAtHeight(1), "a", []byte{byte(i)}, 0)
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

	require.NoError(t, c.Reserve(ctxAtHeight(1), "a", []byte{0x01}, 0))

	// Backdate activity beyond the eviction threshold.
	c.budgets["a"].lastActivity = time.Now().Add(-(types.MaxPaymentPromiseTimeout + 2*time.Hour))
	c.evictIdle()

	require.Empty(t, c.budgets)
	require.Empty(t, c.pending)
}
