package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/test/util/blobfactory"
	"github.com/stretchr/testify/require"
)

func TestPffTxCache(t *testing.T) {
	enc := encoding.MakeConfig(ModuleEncodingRegisters...)
	raw := blobfactory.UnsignedPayForFibreTx(t, enc.TxConfig)
	tx, err := enc.TxConfig.TxDecoder()(raw)
	require.NoError(t, err)

	c := newPffTxCache()

	_, ok := c.Get(raw)
	require.False(t, ok, "empty cache must miss")

	c.Set(raw, tx)
	got, ok := c.Get(raw)
	require.True(t, ok)
	require.Equal(t, tx, got)
	require.Equal(t, 1, c.Len())
	require.Equal(t, int64(len(raw)), c.bytes)

	// Identity is the bytes, not the buffer: a copy hits.
	copied := append([]byte{}, raw...)
	_, ok = c.Get(copied)
	require.True(t, ok)

	// A tx of the same length with one byte changed misses.
	mutated := append([]byte{}, raw...)
	mutated[len(mutated)/2] ^= 0x01
	_, ok = c.Get(mutated)
	require.False(t, ok)

	// Dropping another byte string leaves the entry; dropping the entry's
	// bytes removes it and releases its bytes.
	c.Drop(mutated)
	require.Equal(t, 1, c.Len())
	c.Drop(copied)
	require.Equal(t, 0, c.Len())
	require.Zero(t, c.bytes)

	// Re-setting the same bytes accounts them once.
	c.Set(raw, tx)
	c.Set(raw, tx)
	require.Equal(t, 1, c.Len())
	require.Equal(t, int64(len(raw)), c.bytes)

	c.Set(nil, tx)
	require.Equal(t, 1, c.Len())
}

func TestPffTxCacheByteBound(t *testing.T) {
	enc := encoding.MakeConfig(ModuleEncodingRegisters...)
	raw := blobfactory.UnsignedPayForFibreTx(t, enc.TxConfig)
	tx, err := enc.TxConfig.TxDecoder()(raw)
	require.NoError(t, err)

	// Room for four txs by bytes, many more by count.
	c := newPffTxCacheSized(100, int64(4*len(raw)))
	entries := make([][]byte, 10)
	for i := range entries {
		entries[i] = append([]byte{}, raw...)
		entries[i][len(raw)-1] = byte(i)
		c.Set(entries[i], tx)
		require.LessOrEqual(t, c.bytes, c.maxBytes)
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

	// A tx larger than the whole bound is not recorded at all.
	c.Set(make([]byte, 5*len(raw)), tx)
	require.Equal(t, 4, c.Len())
}
