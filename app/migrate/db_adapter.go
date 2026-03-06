package migrate

import (
	cmtdb "github.com/cometbft/cometbft-db"
	cosmosdb "github.com/cosmos/cosmos-db"
)

// cmtDBAdapter wraps a cometbft-db.DB to implement the cosmos-db.DB interface,
// allowing the migrator to treat all databases uniformly.
// The migrator uses only read methods (Iterator, ReverseIterator) on source databases.
type cmtDBAdapter struct {
	inner cmtdb.DB
}

// WrapCometBFTDB wraps a cometbft-db.DB to satisfy cosmos-db.DB for use as a migration source.
func WrapCometBFTDB(db cmtdb.DB) cosmosdb.DB {
	return &cmtDBAdapter{inner: db}
}

func (a *cmtDBAdapter) Get(key []byte) ([]byte, error)        { return a.inner.Get(key) }
func (a *cmtDBAdapter) Has(key []byte) (bool, error)          { return a.inner.Has(key) }
func (a *cmtDBAdapter) Set(key, value []byte) error           { return a.inner.Set(key, value) }
func (a *cmtDBAdapter) SetSync(key, value []byte) error       { return a.inner.SetSync(key, value) }
func (a *cmtDBAdapter) Delete(key []byte) error               { return a.inner.Delete(key) }
func (a *cmtDBAdapter) DeleteSync(key []byte) error           { return a.inner.DeleteSync(key) }
func (a *cmtDBAdapter) Close() error                          { return a.inner.Close() }
func (a *cmtDBAdapter) Print() error                          { return a.inner.Print() }
func (a *cmtDBAdapter) Stats() map[string]string              { return a.inner.Stats() }
func (a *cmtDBAdapter) NewBatch() cosmosdb.Batch              { return &cmtBatchAdapter{inner: a.inner.NewBatch()} }
func (a *cmtDBAdapter) NewBatchWithSize(_ int) cosmosdb.Batch { return a.NewBatch() }

func (a *cmtDBAdapter) Iterator(start, end []byte) (cosmosdb.Iterator, error) {
	return a.inner.Iterator(start, end)
}

func (a *cmtDBAdapter) ReverseIterator(start, end []byte) (cosmosdb.Iterator, error) {
	return a.inner.ReverseIterator(start, end)
}

// cmtBatchAdapter wraps cometbft-db.Batch to add the GetByteSize method required by cosmos-db.Batch.
type cmtBatchAdapter struct {
	inner cmtdb.Batch
	size  int
}

func (b *cmtBatchAdapter) Set(key, value []byte) error {
	b.size += len(key) + len(value)
	return b.inner.Set(key, value)
}

func (b *cmtBatchAdapter) Delete(key []byte) error {
	b.size += len(key)
	return b.inner.Delete(key)
}

func (b *cmtBatchAdapter) Write() error              { return b.inner.Write() }
func (b *cmtBatchAdapter) WriteSync() error          { return b.inner.WriteSync() }
func (b *cmtBatchAdapter) Close() error              { return b.inner.Close() }
func (b *cmtBatchAdapter) GetByteSize() (int, error) { return b.size, nil }
