package db

import (
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

func (w *PebbleDBWrapper) Get(key []byte) ([]byte, error) {
	return w.Get(key)
}

func (w *PebbleDBWrapper) Has(key []byte) (bool, error) {
	return w.Has(key)
}

func (w *PebbleDBWrapper) Set(key, value []byte) error {
	return w.Set(key, value)
}

func (w *PebbleDBWrapper) SetSync(key, value []byte) error {
	return w.SetSync(key, value)
}

func (w *PebbleDBWrapper) Delete(key []byte) error {
	return w.Delete(key)
}

func (w *PebbleDBWrapper) DeleteSync(key []byte) error {
	return w.DeleteSync(key)
}

func (w *PebbleDBWrapper) Close() error {
	return w.Close()
}

func (w *PebbleDBWrapper) NewBatch() tmdb.Batch {
	return &batchWrapper{w.NewBatch()}
}

func (w *PebbleDBWrapper) Iterator(start, end []byte) (tmdb.Iterator, error) {
	itr, err := w.Iterator(start, end)
	if err != nil {
		return nil, err
	}
	return &iteratorWrapper{itr}, nil
}

func (w *PebbleDBWrapper) ReverseIterator(start, end []byte) (tmdb.Iterator, error) {
	itr, err := w.ReverseIterator(start, end)
	if err != nil {
		return nil, err
	}
	return &iteratorWrapper{itr}, nil
}

func (w *PebbleDBWrapper) Print() error {
	return w.Print()
}

func (w *PebbleDBWrapper) Stats() map[string]string {
	return w.Stats()
}

// Compact triggers manual compaction on the underlying database.
func (w *PebbleDBWrapper) Compact(start, end []byte) error {
	return w.Compact(start, end)
}

// batchWrapper adapts a cometbft-db Batch to the tm-db Batch interface.
type batchWrapper struct {
	cdb.Batch
}

var _ tmdb.Batch = (*batchWrapper)(nil)

func (b *batchWrapper) Set(key, value []byte) error {
	return b.Set(key, value)
}

func (b *batchWrapper) Delete(key []byte) error {
	return b.Delete(key)
}

func (b *batchWrapper) Write() error {
	return b.Write()
}

func (b *batchWrapper) WriteSync() error {
	return b.WriteSync()
}

func (b *batchWrapper) Close() error {
	return b.Close()
}

// iteratorWrapper adapts a cometbft-db Iterator to the tm-db Iterator
// interface. Key() and Value() copy the returned slices because PebbleDB
// reuses internal buffers across Next() calls — without copying, IAVL and
// the Cosmos SDK store layer would see corrupted data.
type iteratorWrapper struct {
	cdb.Iterator
}

var _ tmdb.Iterator = (*iteratorWrapper)(nil)

func (i *iteratorWrapper) Domain() (start []byte, end []byte) {
	s, e := i.Domain()
	return cp(s), cp(e)
}

func (i *iteratorWrapper) Valid() bool {
	return i.Valid()
}

func (i *iteratorWrapper) Next() {
	i.Next()
}

func (i *iteratorWrapper) Key() []byte {
	return cp(i.Key())
}

func (i *iteratorWrapper) Value() []byte {
	return cp(i.Value())
}

func (i *iteratorWrapper) Error() error {
	return i.Error()
}

func (i *iteratorWrapper) Close() error {
	return i.Close()
}

// cp returns a copy of a byte slice. Returns nil for nil input.
func cp(bz []byte) []byte {
	if bz == nil {
		return nil
	}
	out := make([]byte, len(bz))
	copy(out, bz)
	return out
}
