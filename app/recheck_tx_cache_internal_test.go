package app

import (
	"bytes"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// fakeTx stands in for a decoded tx; only its identity matters here.
type fakeTx struct {
	sdk.Tx
	raw []byte
}

func baseDecoder(raw []byte) (sdk.Tx, error) { return &fakeTx{raw: raw}, nil }

func TestTxHandoffLendsOnlyTheSameSlice(t *testing.T) {
	h := &txHandoff{}
	dec := h.decoder(baseDecoder)
	raw := []byte("tx bytes")
	lent := &fakeTx{}

	release := h.lend(raw, lent)
	got, err := dec(raw)
	require.NoError(t, err)
	require.Same(t, lent, got)

	// Equal bytes in another slice, and a sub-slice of the lent one, decode afresh.
	for _, other := range [][]byte{bytes.Clone(raw), raw[:len(raw)-1]} {
		got, err = dec(other)
		require.NoError(t, err)
		require.NotSame(t, lent, got)
	}

	release()
	got, err = dec(raw)
	require.NoError(t, err)
	require.NotSame(t, lent, got)

	// An empty slice is never lent.
	h.lend(nil, lent)
	require.Nil(t, h.lent.Load())
}

func TestRecheckTxCacheSweep(t *testing.T) {
	c := newRecheckTxCache(10)
	a, b := []byte("a"), []byte("b")
	c.add(a, &fakeTx{})
	c.add(b, &fakeTx{})

	// Admitted before the first Commit: both survive it.
	c.sweep()
	require.Len(t, c.entries, 2)

	// Only a is rechecked after that Commit, so the next one drops b.
	_, ok := c.get(a)
	require.True(t, ok)
	c.sweep()
	require.Contains(t, c.entries, "a")
	require.NotContains(t, c.entries, "b")

	// Unused for a whole height, a goes too.
	c.sweep()
	require.Empty(t, c.entries)
}

func TestRecheckTxCacheBoundsAndDrop(t *testing.T) {
	c := newRecheckTxCache(2)
	first := &fakeTx{}
	c.add([]byte("a"), first)
	c.add([]byte("b"), &fakeTx{})
	c.add([]byte("c"), &fakeTx{})
	require.Len(t, c.entries, 2)
	require.NotContains(t, c.entries, "c")

	// Re-adding a cached tx keeps its decode and refreshes it.
	c.sweep()
	c.add([]byte("a"), &fakeTx{})
	got, ok := c.get([]byte("a"))
	require.True(t, ok)
	require.Same(t, first, got)
	c.sweep()
	require.Contains(t, c.entries, "a")

	c.drop([]byte("a"))
	_, ok = c.get([]byte("a"))
	require.False(t, ok)
}

func TestRecheckTxCacheByteBudget(t *testing.T) {
	c := newRecheckTxCache(4)
	budget := 4 * recheckTxBytesPerEntry
	big := bytes.Repeat([]byte{1}, budget-1)
	c.add(big, &fakeTx{})
	require.Equal(t, budget-1, c.bytes)

	// Two more bytes would cross the budget; one more fits it exactly.
	c.add([]byte("ab"), &fakeTx{})
	require.Len(t, c.entries, 1)
	c.add([]byte("a"), &fakeTx{})
	require.Len(t, c.entries, 2)

	// drop and sweep give the bytes back; dropping a missing entry does not.
	c.drop([]byte("a"))
	c.drop([]byte("a"))
	require.Equal(t, budget-1, c.bytes)
	c.sweep()
	c.sweep()
	require.Empty(t, c.entries)
	require.Zero(t, c.bytes)
}
