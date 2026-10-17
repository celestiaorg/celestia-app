package sigcache_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	"github.com/stretchr/testify/require"
)

// TestNewKeySeparatesDomains pins the reason Domain exists: the same bytes
// verified as two different things must not share an entry, so a promise
// signature can never satisfy a certificate lookup.
func TestNewKeySeparatesDomains(t *testing.T) {
	parts := [][]byte{[]byte("pubkey"), []byte("message"), []byte("signature")}

	certificate := sigcache.NewKey(sigcache.PffCertificate, parts...)
	promise := sigcache.NewKey(sigcache.PromiseSignature, parts...)
	transaction := sigcache.NewKey(sigcache.TxSignature, parts...)

	require.NotEqual(t, certificate, promise)
	require.NotEqual(t, promise, transaction)
	require.NotEqual(t, certificate, transaction)
}

// TestNewKeySeparatesPartBoundaries pins the length prefix: two different
// splits of the same concatenated bytes must not collide, or a caller could
// move bytes from one field to the next and reuse an entry.
func TestNewKeySeparatesPartBoundaries(t *testing.T) {
	require.NotEqual(t,
		sigcache.NewKey(sigcache.TxSignature, []byte("ab"), []byte("c")),
		sigcache.NewKey(sigcache.TxSignature, []byte("a"), []byte("bc")),
	)
	require.NotEqual(t,
		sigcache.NewKey(sigcache.TxSignature, []byte("abc")),
		sigcache.NewKey(sigcache.TxSignature, []byte("abc"), nil),
	)
	require.NotEqual(t,
		sigcache.NewKey(sigcache.TxSignature),
		sigcache.NewKey(sigcache.TxSignature, nil),
	)
}

// TestNewKeyIsDeterministic pins that the same inputs key the same entry, which
// is the whole point: a second verification of the same signature must hit.
func TestNewKeyIsDeterministic(t *testing.T) {
	first := sigcache.NewKey(sigcache.PromiseSignature, []byte("pubkey"), []byte("sign bytes"))
	second := sigcache.NewKey(sigcache.PromiseSignature, []byte("pubkey"), []byte("sign bytes"))
	require.Equal(t, first, second)
}

func TestCacheAddAndHas(t *testing.T) {
	cache := sigcache.New(8)
	key := sigcache.NewKey(sigcache.PffCertificate, []byte("certificate"))

	require.False(t, cache.Has(key))
	require.Zero(t, cache.Len())

	cache.Add(key)
	require.True(t, cache.Has(key))
	require.Equal(t, 1, cache.Len())

	// Adding the same key again is not a second entry.
	cache.Add(key)
	require.Equal(t, 1, cache.Len())

	require.False(t, cache.Has(sigcache.NewKey(sigcache.PffCertificate, []byte("other"))))
}

// TestCacheEvictsLeastRecentlyUsed pins that the capacity is a bound and that
// losing an entry costs a re-verification, never a wrong answer: the evicted
// key simply misses.
func TestCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := sigcache.New(2)
	first := sigcache.NewKey(sigcache.TxSignature, []byte("first"))
	second := sigcache.NewKey(sigcache.TxSignature, []byte("second"))
	third := sigcache.NewKey(sigcache.TxSignature, []byte("third"))

	cache.Add(first)
	cache.Add(second)
	cache.Add(third)

	require.Equal(t, 2, cache.Len())
	require.False(t, cache.Has(first))
	require.True(t, cache.Has(second))
	require.True(t, cache.Has(third))
}

// TestNewPanicsOnZeroCapacity documents why every caller computing a capacity
// has to floor it: the underlying LRU rejects zero, and app.mempoolMaxTxs
// exists to keep an unparseable mempool.size from reaching here.
func TestNewPanicsOnZeroCapacity(t *testing.T) {
	require.Panics(t, func() { sigcache.New(0) })
}
