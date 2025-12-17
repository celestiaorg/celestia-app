package fibre

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	ds "github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	dssync "github.com/ipfs/go-datastore/sync"
	badger "github.com/ipfs/go-ds-badger4"
)

// ErrStoreNotFound is returned when no shard is found for a [Commitment] in the [Store].
var ErrStoreNotFound = errors.New("no shard found in store")

// StoreConfig contains configuration options for the [Store].
type StoreConfig struct {
	// Path is the path to the store directory.
	Path string
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
func NewBadgerStore(cfg StoreConfig) (*Store, error) {
	opts := badger.DefaultOptions
	opts.GcDiscardRatio = 0.2
	opts.GcSleep = time.Second
	opts.GcInterval = time.Minute

	bds, err := badger.NewDatastore(cfg.Path, &opts)
	if err != nil {
		return nil, fmt.Errorf("creating badger datastore: %w", err)
	}

	return &Store{
		cfg: cfg,
		ds:  bds,
	}, nil
}

// Put stores given [PaymentPromise] and [types.BlobShard].
//
// Shards are stored as a single blob under /shard/<commitment>/<promise-hash>.
// The payment promise is stored under /pp/<promise-hash>.
// The pruneAt sets the timestamp used for the /prune/<YYYYMMDDHHmm>/<commitment>/<promise-hash>,
// determining when the entry will be removed by [PruneBefore].
//
// Puts for the same commitments but different promises are allowed and are stored independently without deduplication.
func (s *Store) Put(ctx context.Context, promise *PaymentPromise, shard *types.BlobShard, pruneAt time.Time) error {
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

	// write shard
	shardData, err := gogoproto.Marshal(shard)
	if err != nil {
		return fmt.Errorf("marshaling shard: %w", err)
	}
	if err := batch.Put(ctx, shardKey(promise.Commitment, promiseHash), shardData); err != nil {
		return fmt.Errorf("putting shard: %w", err)
	}

	// write prune index
	if err := batch.Put(ctx, pruneKey(pruneAt, promise.Commitment, promiseHash), []byte{}); err != nil {
		return fmt.Errorf("putting prune index: %w", err)
	}

	return batch.Commit(ctx)
}

// Get retrieves [types.BlobShard] for the given [Commitment].
//
// When multiple payment promises exist for the same commitment
// this method combines all their rows into a single [types.BlobShard] result.
//
// If unmarshaling fails for some entries, it continues trying others and collects errors.
// Returns an error only if all entries fail to unmarshal or if no shards are found.
func (s *Store) Get(ctx context.Context, commitment Commitment) (*types.BlobShard, error) {
	results, err := s.ds.Query(ctx, query.Query{
		Prefix: fmt.Sprintf("/shard/%s", commitment.String()),
	})
	if err != nil {
		return nil, fmt.Errorf("querying shards: %w", err)
	}
	defer results.Close()

	var (
		combinedShard *types.BlobShard
		rerr          error
	)

	// collect all rows from all promises with this commitment
	for result := range results.Next() {
		if result.Error != nil {
			rerr = errors.Join(rerr, result.Error)
			continue
		}

		shard := &types.BlobShard{}
		if err := gogoproto.Unmarshal(result.Value, shard); err != nil {
			rerr = errors.Join(rerr, fmt.Errorf("unmarshaling shard: %w", err))
			continue
		}

		if combinedShard == nil {
			combinedShard = shard
			continue
		}

		// append all rows from this entry
		combinedShard.Rows = append(combinedShard.Rows, shard.Rows...)
	}
	if combinedShard != nil {
		return combinedShard, nil
	}
	// if we have no shards at all, return error
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

// PruneBefore deletes all shards and payment promises with pruneAt before the given time
// and returns the number of pruned entries.
//
// It works by iterating over the ordered prune index and deleting each entry until the given time,
// so it iterates exactly over the entries that need to be pruned. The order is guaranteed by the
// underlying database and enforced with query.OrderByKey{}.
func (s *Store) PruneBefore(ctx context.Context, before time.Time) (int, error) {
	results, err := s.ds.Query(ctx, query.Query{
		Prefix:   "/prune/",
		KeysOnly: true,
		Orders:   []query.Order{query.OrderByKey{}},
	})
	if err != nil {
		return 0, fmt.Errorf("querying prune index: %w", err)
	}
	defer results.Close()

	batch, err := s.ds.Batch(ctx)
	if err != nil {
		return 0, fmt.Errorf("creating batch: %w", err)
	}

	pruned := 0
	beforeStr := formatTimestamp(before)
	for result := range results.Next() {
		if result.Error != nil {
			return pruned, fmt.Errorf("iterating results: %w", result.Error)
		}

		// extract timestamp from key: /prune/YYYYMMDDHHmm/...
		// early termination: keys are sorted, so if timestamp >= cutoff, we're done
		timestampStr := result.Key[7:19] // skip "/prune/" and take 12 chars
		if timestampStr >= beforeStr {
			break
		}

		// parse key: /prune/<timestamp>/<commitment>/<promise-hash>
		commitment, promiseHash, ok := parsePruneKey(result.Key)
		if !ok {
			continue
		}

		// delete all related entries
		if err := batch.Delete(ctx, ds.NewKey(result.Key)); err != nil {
			return pruned, fmt.Errorf("deleting prune index: %w", err)
		}
		if err := batch.Delete(ctx, shardKey(commitment, promiseHash)); err != nil {
			return pruned, fmt.Errorf("deleting shard: %w", err)
		}
		if err := batch.Delete(ctx, promiseKey(promiseHash)); err != nil {
			return pruned, fmt.Errorf("deleting payment promise: %w", err)
		}
		pruned++
	}

	if err := batch.Commit(ctx); err != nil {
		return pruned, fmt.Errorf("committing batch: %w", err)
	}
	return pruned, nil
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

func shardKey(commitment Commitment, promiseHash []byte) ds.Key {
	return ds.NewKey(fmt.Sprintf("/shard/%s/%s", commitment.String(), hex.EncodeToString(promiseHash)))
}

func pruneKey(pruneAt time.Time, commitment Commitment, promiseHash []byte) ds.Key {
	return ds.NewKey(fmt.Sprintf("/prune/%s/%s/%s", formatTimestamp(pruneAt), commitment.String(), hex.EncodeToString(promiseHash)))
}

// parsePruneKey extracts commitment and promise hash from a prune index key.
// Key format: /prune/<timestamp>/<commitment>/<promise-hash>
func parsePruneKey(key string) (Commitment, []byte, bool) {
	// split: ["", "prune", "<timestamp>", "<commitment>", "<promise-hash>"]
	parts := strings.Split(key, "/")
	if len(parts) != 5 {
		return Commitment{}, nil, false
	}

	commitment, err := CommitmentFromString(parts[3])
	if err != nil {
		return Commitment{}, nil, false
	}

	promiseHash, err := hex.DecodeString(parts[4])
	if err != nil {
		return Commitment{}, nil, false
	}

	return commitment, promiseHash, true
}
