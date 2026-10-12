package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// stubAppOptions returns one configured value and nil for everything else.
type stubAppOptions struct {
	key   string
	value any
}

func (o stubAppOptions) Get(key string) any {
	if key == o.key {
		return o.value
	}
	return nil
}

func TestMempoolMaxTxs(t *testing.T) {
	testCases := []struct {
		name     string
		value    any
		expected int
	}{
		{name: "configured", value: 20_000, expected: 20_000},
		{name: "configured as string", value: "20000", expected: 20_000},
		{name: "key absent", value: nil, expected: DefaultMempoolMaxTxs},
		{name: "unparseable", value: "not a number", expected: DefaultMempoolMaxTxs},
		{name: "zero", value: 0, expected: DefaultMempoolMaxTxs},
		{name: "negative", value: -1, expected: DefaultMempoolMaxTxs},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			opts := stubAppOptions{key: "mempool.size", value: tc.value}
			require.Equal(t, tc.expected, mempoolMaxTxs(opts))
		})
	}
}

func TestMempoolMaxTxsNilAppOptions(t *testing.T) {
	require.Equal(t, DefaultMempoolMaxTxs, mempoolMaxTxs(nil))
}

func TestSigCacheCapacity(t *testing.T) {
	// Three entries per transaction plus 20% headroom.
	require.Equal(t, 18_000, SigCacheCapacity(DefaultMempoolMaxTxs))
	require.Equal(t, 72_000, SigCacheCapacity(20_000))

	// A block's worth of PayForFibre messages fits many times over, so the
	// entries a proposal writes survive until the next phase reads them.
	perBlock := sigCacheEntriesPerTx * 200 // appconsts.MaxPayForFibreMessages
	require.Greater(t, SigCacheCapacity(DefaultMempoolMaxTxs), perBlock*10)
}

func TestNewSigCacheIsUsable(t *testing.T) {
	cache := NewSigCache(DefaultMempoolMaxTxs)
	require.NotNil(t, cache)
	require.Equal(t, 0, cache.Len())
}
