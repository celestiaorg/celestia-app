package fibre

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	ds "github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	dssync "github.com/ipfs/go-datastore/sync"
	badger "github.com/ipfs/go-ds-badger4"
)

// ErrStoreNotFound is returned when no rows are found for a [Commitment] in the [Store].
var ErrStoreNotFound = errors.New("no rows found in store")

// StoreConfig contains configuration options for the [Store].
type StoreConfig struct {
	// DataRetentionDuration defines how long uploaded blob data is retained.
	// Data older than this duration will be automatically deleted by TTL expiration.
	DataRetentionDuration time.Duration
	// PaymentPromiseTimeout defines how long payment promises are retained.
	// Promises older than this duration will be automatically deleted by TTL expiration.
	PaymentPromiseTimeout time.Duration
}

// DefaultStoreConfig returns a [StoreConfig] with default values.
func DefaultStoreConfig() StoreConfig {
	return StoreConfig{
		DataRetentionDuration: 24 * time.Hour,
		PaymentPromiseTimeout: 1 * time.Hour,
	}
}

// Store manages persistent storage of [PaymentPromise] and row data.
// It provides indexed access by [Commitment], promise hash, and timestamp.
// TODO(@Wondertan): GC logic
type Store struct {
	cfg StoreConfig
	ds  ds.Batching
}

// NewMemoryStore creates a new [Store] with an in-memory datastore.
func NewMemoryStore(cfg StoreConfig) *Store {
	return &Store{
		cfg: cfg,
		ds:  dssync.MutexWrap(ds.NewMapDatastore()),
	}
}

// NewBadgerStore creates a new [Store] with a badger4 datastore at the given path.
func NewBadgerStore(path string, cfg StoreConfig) (*Store, error) {
	opts := badger.DefaultOptions
	opts.GcDiscardRatio = 0.2
	opts.GcSleep = time.Second
	opts.GcInterval = time.Minute

	bds, err := badger.NewDatastore(path, &opts)
	if err != nil {
		return nil, fmt.Errorf("creating badger datastore: %w", err)
	}

	return &Store{
		cfg: cfg,
		ds:  bds,
	}, nil
}

// Put stores given [PaymentPromise] and [types.Rows].
//
// Rows are stored as a single blob under /rows/<commitment>/<promise-hash>.
// The payment promise is stored under /pp/<promise-hash>.
// An empty value is indexed under /tp/<timestamp-YYYYMMDDHHmm>/<commitment>/<promise-hash> for time-based queries.
//
// Puts for the same commitments but different promises are allowed and are stored independently without deduplication.
func (s *Store) Put(ctx context.Context, promise *PaymentPromise, rows *types.Rows) error {
	batch, err := s.ds.Batch(ctx)
	if err != nil {
		return fmt.Errorf("creating batch: %w", err)
	}

	// write payment promise
	ppData, err := gogoproto.Marshal(promise.ToProto())
	if err != nil {
		return fmt.Errorf("marshaling payment promise: %w", err)
	}
	promiseHash, err := promise.Hash()
	if err != nil {
		return fmt.Errorf("getting promise hash: %w", err)
	}
	if err := batch.Put(ctx, promiseKey(promiseHash), ppData); err != nil {
		return fmt.Errorf("putting payment promise: %w", err)
	}

	// write rows
	rowsData, err := gogoproto.Marshal(rows)
	if err != nil {
		return fmt.Errorf("marshaling rows: %w", err)
	}
	if err := batch.Put(ctx, rowsKey(promise.Commitment, promiseHash), rowsData); err != nil {
		return fmt.Errorf("putting rows: %w", err)
	}

	// write timestamp index
	if err := batch.Put(ctx, timestampKey(promise.CreationTimestamp, promise.Commitment, promiseHash), []byte{}); err != nil {
		return fmt.Errorf("putting timestamp index: %w", err)
	}

	return batch.Commit(ctx)
}

// Get retrieves [types.Rows] for the given [Commitment].
//
// When multiple payment promises exist for the same commitment
// this method combines all their rows into a single [types.Rows] result.
//
// If unmarshaling fails for some entries, it continues trying others and collects errors.
// Returns an error only if all entries fail to unmarshal or if no rows are found.
func (s *Store) Get(ctx context.Context, commitment Commitment) (*types.Rows, error) {
	results, err := s.ds.Query(ctx, query.Query{
		Prefix: fmt.Sprintf("/rows/%s", commitment.String()),
	})
	if err != nil {
		return nil, fmt.Errorf("querying rows: %w", err)
	}
	defer results.Close()

	var (
		combinedRows *types.Rows
		rerr         error
	)

	// collect all rows from all promises with this commitment
	for result := range results.Next() {
		if result.Error != nil {
			rerr = errors.Join(rerr, result.Error)
			continue
		}

		rows := &types.Rows{}
		if err := gogoproto.Unmarshal(result.Value, rows); err != nil {
			rerr = errors.Join(rerr, fmt.Errorf("unmarshaling rows: %w", err))
			continue
		}

		if combinedRows == nil {
			combinedRows = rows
			continue
		}

		// append all rows from this entry
		combinedRows.Rows = append(combinedRows.Rows, rows.Rows...)
	}
	if combinedRows != nil {
		return combinedRows, nil
	}
	// if we have no rows at all, return error
	if rerr != nil {
		return nil, rerr
	}
	return nil, ErrStoreNotFound
}

// GetPaymentPromise retrieves a [PaymentPromise] by its hash.
func (s *Store) GetPaymentPromise(ctx context.Context, promiseHash []byte) (*PaymentPromise, error) {
	data, err := s.ds.Get(ctx, promiseKey(promiseHash))
	if err != nil {
		return nil, fmt.Errorf("getting payment promise: %w", err)
	}

	var ppProto types.PaymentPromise
	if err := gogoproto.Unmarshal(data, &ppProto); err != nil {
		return nil, fmt.Errorf("unmarshaling payment promise: %w", err)
	}

	var promise PaymentPromise
	if err := promise.FromProto(&ppProto); err != nil {
		return nil, fmt.Errorf("converting from proto: %w", err)
	}

	return &promise, nil
}

// Close closes the underlying datastore.
func (s *Store) Close() error {
	return s.ds.Close()
}

// formatTimestamp formats a timestamp with minute precision (YYYYMMDDHHmm).
// This format is used for timestamp-based indexing in the datastore.
func formatTimestamp(timestamp time.Time) string {
	return timestamp.Format("200601021504")
}

func promiseKey(promiseHash []byte) ds.Key {
	return ds.NewKey(fmt.Sprintf("/pp/%s", hex.EncodeToString(promiseHash)))
}

func rowsKey(commitment Commitment, promiseHash []byte) ds.Key {
	return ds.NewKey(fmt.Sprintf("/rows/%s/%s", commitment.String(), hex.EncodeToString(promiseHash)))
}

func timestampKey(timestamp time.Time, commitment Commitment, promiseHash []byte) ds.Key {
	return ds.NewKey(fmt.Sprintf("/tp/%s/%s/%s", formatTimestamp(timestamp), commitment.String(), hex.EncodeToString(promiseHash)))
}
