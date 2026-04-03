package dbcompat

import (
	cdb "github.com/cometbft/cometbft-db"
	tmdb "github.com/tendermint/tm-db"
)

type BackendType = tmdb.BackendType

const (
	GoLevelDBBackend BackendType = tmdb.GoLevelDBBackend
	CLevelDBBackend  BackendType = tmdb.CLevelDBBackend
	MemDBBackend     BackendType = tmdb.MemDBBackend
	BoltDBBackend    BackendType = tmdb.BoltDBBackend
	RocksDBBackend   BackendType = tmdb.RocksDBBackend
	BadgerDBBackend  BackendType = tmdb.BadgerDBBackend
)

// Wrap exposes a cometbft-db database through the tm-db interfaces expected by
// Celestia App v3.x and Cosmos SDK v0.46.
func Wrap(db cdb.DB) tmdb.DB {
	return &dbAdapter{db: db}
}

// NewDB constructs a cometbft-db backend and wraps it with tm-db-compatible
// interfaces for the app layer.
func NewDB(name string, backend tmdb.BackendType, dir string) (tmdb.DB, error) {
	db, err := cdb.NewDB(name, cdb.BackendType(backend), dir)
	if err != nil {
		return nil, err
	}

	return Wrap(db), nil
}

// NewGoLevelDB constructs a wrapped goleveldb database.
func NewGoLevelDB(name string, dir string) (tmdb.DB, error) {
	db, err := cdb.NewGoLevelDB(name, dir)
	if err != nil {
		return nil, err
	}

	return Wrap(db), nil
}

// NewMemDB constructs a wrapped in-memory database.
func NewMemDB() tmdb.DB {
	return Wrap(cdb.NewMemDB())
}

type dbAdapter struct {
	db cdb.DB
}

var _ tmdb.DB = (*dbAdapter)(nil)

func (d *dbAdapter) Get(key []byte) ([]byte, error) {
	return d.db.Get(key)
}

func (d *dbAdapter) Has(key []byte) (bool, error) {
	return d.db.Has(key)
}

func (d *dbAdapter) Set(key, value []byte) error {
	return d.db.Set(key, value)
}

func (d *dbAdapter) SetSync(key, value []byte) error {
	return d.db.SetSync(key, value)
}

func (d *dbAdapter) Delete(key []byte) error {
	return d.db.Delete(key)
}

func (d *dbAdapter) DeleteSync(key []byte) error {
	return d.db.DeleteSync(key)
}

func (d *dbAdapter) Close() error {
	return d.db.Close()
}

func (d *dbAdapter) NewBatch() tmdb.Batch {
	return &batchAdapter{batch: d.db.NewBatch()}
}

func (d *dbAdapter) Iterator(start, end []byte) (tmdb.Iterator, error) {
	itr, err := d.db.Iterator(start, end)
	if err != nil {
		return nil, err
	}

	return &iteratorAdapter{itr: itr}, nil
}

func (d *dbAdapter) ReverseIterator(start, end []byte) (tmdb.Iterator, error) {
	itr, err := d.db.ReverseIterator(start, end)
	if err != nil {
		return nil, err
	}

	return &iteratorAdapter{itr: itr}, nil
}

func (d *dbAdapter) Print() error {
	return d.db.Print()
}

func (d *dbAdapter) Stats() map[string]string {
	return d.db.Stats()
}

type batchAdapter struct {
	batch cdb.Batch
}

var _ tmdb.Batch = (*batchAdapter)(nil)

func (b *batchAdapter) Set(key, value []byte) error {
	return b.batch.Set(key, value)
}

func (b *batchAdapter) Delete(key []byte) error {
	return b.batch.Delete(key)
}

func (b *batchAdapter) Write() error {
	return b.batch.Write()
}

func (b *batchAdapter) WriteSync() error {
	return b.batch.WriteSync()
}

func (b *batchAdapter) Close() error {
	return b.batch.Close()
}

type iteratorAdapter struct {
	itr cdb.Iterator
}

var _ tmdb.Iterator = (*iteratorAdapter)(nil)

func (i *iteratorAdapter) Domain() (start []byte, end []byte) {
	iterStart, iterEnd := i.itr.Domain()
	return copyBytes(iterStart), copyBytes(iterEnd)
}

func (i *iteratorAdapter) Valid() bool {
	return i.itr.Valid()
}

func (i *iteratorAdapter) Next() {
	i.itr.Next()
}

func (i *iteratorAdapter) Key() []byte {
	return copyBytes(i.itr.Key())
}

func (i *iteratorAdapter) Value() []byte {
	return copyBytes(i.itr.Value())
}

func (i *iteratorAdapter) Error() error {
	return i.itr.Error()
}

func (i *iteratorAdapter) Close() error {
	return i.itr.Close()
}

func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}

	out := make([]byte, len(b))
	copy(out, b)
	return out
}
