package db

import (
	"slices"

	cdb "github.com/cometbft/cometbft-db"
	tmdb "github.com/tendermint/tm-db"
)

const PebbleDBBackend tmdb.BackendType = "pebbledb"

// Compactor is an optional interface for databases that support compaction.
type Compactor interface {
	Compact(start, end []byte) error
}

// NewPebbleDB creates a PebbleDB-backed database wrapped to satisfy the
// tm-db DB interface expected by Cosmos SDK v0.46.
func NewPebbleDB(name, dir string) (tmdb.DB, error) {
	db, err := cdb.NewPebbleDB(name, dir)
	if err != nil {
		return nil, err
	}
	return &PebbleDBWrapper{db}, nil
}

// PebbleDBWrapper adapts a cometbft-db DB (PebbleDB) to the tm-db DB
// interface. Iterator Key/Value calls copy the returned byte slices to
// preserve the tm-db contract that returned slices are stable across
// iterator advances.
type PebbleDBWrapper struct {
	cdb.DB
}

var _ tmdb.DB = (*PebbleDBWrapper)(nil)

// NewBatch wraps the inner batch to return a tm-db Batch.
func (w *PebbleDBWrapper) NewBatch() tmdb.Batch {
	return &batchWrapper{w.DB.NewBatch()}
}

// Iterator wraps the inner iterator to copy Key/Value on each call.
func (w *PebbleDBWrapper) Iterator(start, end []byte) (tmdb.Iterator, error) {
	itr, err := w.DB.Iterator(start, end)
	if err != nil {
		return nil, err
	}
	return &iteratorWrapper{itr}, nil
}

// ReverseIterator wraps the inner iterator to copy Key/Value on each call.
func (w *PebbleDBWrapper) ReverseIterator(start, end []byte) (tmdb.Iterator, error) {
	itr, err := w.DB.ReverseIterator(start, end)
	if err != nil {
		return nil, err
	}
	return &iteratorWrapper{itr}, nil
}

// batchWrapper adapts a cometbft-db Batch to the tm-db Batch interface.
type batchWrapper struct {
	cdb.Batch
}

var _ tmdb.Batch = (*batchWrapper)(nil)

// iteratorWrapper adapts a cometbft-db Iterator to the tm-db Iterator
// interface. Key() and Value() copy the returned slices because PebbleDB
// reuses internal buffers across Next() calls — without copying, IAVL and
// the Cosmos SDK store layer would see corrupted data.
type iteratorWrapper struct {
	cdb.Iterator
}

var _ tmdb.Iterator = (*iteratorWrapper)(nil)

func (i *iteratorWrapper) Domain() (start []byte, end []byte) {
	s, e := i.Iterator.Domain()
	return slices.Clone(s), slices.Clone(e)
}

func (i *iteratorWrapper) Key() []byte {
	return slices.Clone(i.Iterator.Key())
}

func (i *iteratorWrapper) Value() []byte {
	return slices.Clone(i.Iterator.Value())
}
