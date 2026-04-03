package dbcompat

import (
	"testing"

	cdb "github.com/cometbft/cometbft-db"
	"github.com/stretchr/testify/require"
)

func TestIteratorCopiesKeyAndValue(t *testing.T) {
	dir := t.TempDir()

	db, err := cdb.NewDB("iterator-copy", cdb.PebbleDBBackend, dir)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	require.NoError(t, db.Set([]byte("a"), []byte("one")))
	require.NoError(t, db.Set([]byte("b"), []byte("two")))

	wrapped := Wrap(db)
	itr, err := wrapped.Iterator(nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, itr.Close())
	})

	require.True(t, itr.Valid())
	key := itr.Key()
	value := itr.Value()

	itr.Next()
	require.True(t, itr.Valid())

	require.Equal(t, []byte("a"), key)
	require.Equal(t, []byte("one"), value)
	require.Equal(t, []byte("b"), itr.Key())
	require.Equal(t, []byte("two"), itr.Value())
}

func TestWrappedDBBehavesLikeTMDB(t *testing.T) {
	db := NewMemDB()

	require.NoError(t, db.Set([]byte("key"), []byte("value")))

	got, err := db.Get([]byte("key"))
	require.NoError(t, err)
	require.Equal(t, []byte("value"), got)

	has, err := db.Has([]byte("key"))
	require.NoError(t, err)
	require.True(t, has)
}
