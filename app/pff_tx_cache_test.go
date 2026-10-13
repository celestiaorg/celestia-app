package app

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPffTxCache(t *testing.T) {
	raw := []byte("a payforfibre tx")
	signer := pffSignerData{addr: bytes.Repeat([]byte{0xab}, 20), seq: 7}

	c := newPffTxCache()

	_, ok := c.Get(raw)
	require.False(t, ok, "empty cache must miss")

	c.Set(raw, signer)
	got, ok := c.Get(raw)
	require.True(t, ok)
	require.Equal(t, signer, got)
	require.Equal(t, 1, c.Len())

	// Identity is the bytes, not the buffer: a copy hits.
	copied := bytes.Clone(raw)
	_, ok = c.Get(copied)
	require.True(t, ok)

	// A tx of the same length with one byte changed misses.
	mutated := bytes.Clone(raw)
	mutated[len(mutated)/2] ^= 0x01
	_, ok = c.Get(mutated)
	require.False(t, ok)

	// The entry is not reachable through what Get returned.
	got.addr[0] ^= 0xff
	again, ok := c.Get(raw)
	require.True(t, ok)
	require.Equal(t, signer, again)

	// Dropping another byte string leaves the entry; dropping the entry's
	// bytes removes it.
	c.Drop(mutated)
	require.Equal(t, 1, c.Len())
	c.Drop(copied)
	require.Equal(t, 0, c.Len())

	// Re-setting the same bytes keeps one entry.
	c.Set(raw, signer)
	c.Set(raw, signer)
	require.Equal(t, 1, c.Len())

	// Nothing to key on, or nothing worth recording.
	c.Set(nil, signer)
	c.Set(raw, pffSignerData{seq: 1})
	require.Equal(t, 1, c.Len())
	got, _ = c.Get(raw)
	require.Equal(t, signer, got)
}

func TestPffTxCacheCapacity(t *testing.T) {
	signer := pffSignerData{addr: bytes.Repeat([]byte{0xab}, 20), seq: 7}
	c := newPffTxCacheSized(4)

	entries := make([][]byte, 10)
	for i := range entries {
		entries[i] = []byte{byte(i)}
		c.Set(entries[i], signer)
		require.LessOrEqual(t, c.Len(), 4)
	}

	require.Equal(t, 4, c.Len())
	for i := range 6 {
		_, ok := c.Get(entries[i])
		require.False(t, ok, "entry %d should have been evicted", i)
	}
	for i := 6; i < 10; i++ {
		_, ok := c.Get(entries[i])
		require.True(t, ok, "entry %d should be present", i)
	}
}
