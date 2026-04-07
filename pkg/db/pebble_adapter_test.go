package db

import (
	"testing"

	cdb "github.com/cometbft/cometbft-db"
	"github.com/stretchr/testify/require"
)

func TestIteratorCopiesKeyAndValue(t *testing.T) {
	dir := t.TempDir()

	db, err := NewPebbleDB("iter-copy-test", dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	require.NoError(t, db.Set([]byte("a"), []byte("one")))
	require.NoError(t, db.Set([]byte("b"), []byte("two")))

	itr, err := db.Iterator(nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, itr.Close()) })

	require.True(t, itr.Valid())
	key1 := itr.Key()
	val1 := itr.Value()

	itr.Next()
	require.True(t, itr.Valid())

	// Original captures must be stable after Next().
	require.Equal(t, []byte("a"), key1)
	require.Equal(t, []byte("one"), val1)
	require.Equal(t, []byte("b"), itr.Key())
	require.Equal(t, []byte("two"), itr.Value())
}

func TestReverseIterator(t *testing.T) {
	dir := t.TempDir()

	db, err := NewPebbleDB("rev-iter-test", dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	require.NoError(t, db.Set([]byte("a"), []byte("1")))
	require.NoError(t, db.Set([]byte("b"), []byte("2")))
	require.NoError(t, db.Set([]byte("c"), []byte("3")))

	itr, err := db.ReverseIterator(nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, itr.Close()) })

	require.True(t, itr.Valid())
	require.Equal(t, []byte("c"), itr.Key())

	itr.Next()
	require.True(t, itr.Valid())
	require.Equal(t, []byte("b"), itr.Key())
}

func TestBatchWriteAndRead(t *testing.T) {
	dir := t.TempDir()

	db, err := NewPebbleDB("batch-test", dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	batch := db.NewBatch()
	require.NoError(t, batch.Set([]byte("k1"), []byte("v1")))
	require.NoError(t, batch.Set([]byte("k2"), []byte("v2")))
	require.NoError(t, batch.Write())
	require.NoError(t, batch.Close())

	v1, err := db.Get([]byte("k1"))
	require.NoError(t, err)
	require.Equal(t, []byte("v1"), v1)

	v2, err := db.Get([]byte("k2"))
	require.NoError(t, err)
	require.Equal(t, []byte("v2"), v2)
}

func TestCompact(t *testing.T) {
	dir := t.TempDir()

	db, err := NewPebbleDB("compact-test", dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	require.NoError(t, db.Set([]byte("a"), []byte("1")))
	require.NoError(t, db.Set([]byte("z"), []byte("2")))

	compactor, ok := db.(Compactor)
	require.True(t, ok)
	require.NoError(t, compactor.Compact(nil, nil))
}

func TestCRUD(t *testing.T) {
	dir := t.TempDir()

	db, err := NewPebbleDB("crud-test", dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	// Set + Get
	require.NoError(t, db.Set([]byte("key"), []byte("val")))
	got, err := db.Get([]byte("key"))
	require.NoError(t, err)
	require.Equal(t, []byte("val"), got)

	// Has
	has, err := db.Has([]byte("key"))
	require.NoError(t, err)
	require.True(t, has)

	// Delete
	require.NoError(t, db.Delete([]byte("key")))
	got, err = db.Get([]byte("key"))
	require.NoError(t, err)
	require.Nil(t, got)

	// SetSync + DeleteSync
	require.NoError(t, db.SetSync([]byte("sync"), []byte("val")))
	got, err = db.Get([]byte("sync"))
	require.NoError(t, err)
	require.Equal(t, []byte("val"), got)

	require.NoError(t, db.DeleteSync([]byte("sync")))
	got, err = db.Get([]byte("sync"))
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestEmptyKeyErrors(t *testing.T) {
	dir := t.TempDir()

	db, err := NewPebbleDB("errkey-test", dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	_, err = db.Get([]byte{})
	require.Error(t, err)

	require.Error(t, db.Set([]byte{}, []byte("v")))
	require.Error(t, db.Delete([]byte{}))
}

func TestNilValueErrors(t *testing.T) {
	dir := t.TempDir()

	db, err := NewPebbleDB("errval-test", dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	require.Error(t, db.Set([]byte("k"), nil))
}

func TestNewPebbleDBRegisteredBackend(t *testing.T) {
	dir := t.TempDir()
	db, err := cdb.NewDB("backend-test", cdb.PebbleDBBackend, dir)
	require.NoError(t, err)
	require.NoError(t, db.Close())
}
