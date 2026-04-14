package fibre

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/vfs"
	gogoproto "github.com/cosmos/gogoproto/proto"
)

// ErrStoreNotFound is returned when no shard is found for a [Commitment] in the [Store].
var ErrStoreNotFound = errors.New("no shard found in store")

// StoreConfig contains configuration options for the [Store].
type StoreConfig struct {
	// Path is the path to the store directory.
	Path string `toml:"-"`
}

// DefaultStoreConfig returns a [StoreConfig] with default values.
func DefaultStoreConfig() StoreConfig {
	return StoreConfig{}
}

// Validate checks that the StoreConfig is valid.
func (cfg StoreConfig) Validate() error {
	if cfg.Path == "" {
		return fmt.Errorf("store path is required")
	}
	return nil
}

// Store manages persistent storage of [PaymentPromise] and row data.
// It provides indexed access by [Commitment], promise hash, and timestamp.
type Store struct {
	cfg StoreConfig
	db  *pebbledb.DB
}

// NewMemoryStore creates a new [Store] with an in-memory Pebble database.
func NewMemoryStore(cfg StoreConfig) *Store {
	db, err := pebbledb.Open("", &pebbledb.Options{FS: vfs.NewMem()})
	if err != nil {
		panic(fmt.Sprintf("opening in-memory pebble: %v", err))
	}
	return &Store{
		cfg: cfg,
		db:  db,
	}
}

// NewPebbleStore creates a new [Store] with a Pebble database at the given path.
// Tuned for FIBRE's use case: large values (32KB rows), bulk writes/reads.
func NewPebbleStore(cfg StoreConfig) (*Store, error) {
	opts := &pebbledb.Options{}

	// MemTable settings - moderate size for bulk writes
	opts.MemTableSize = 16 << 20 // 16 MiB memtable (default 4 MiB)

	// L0 compaction settings - reduce write stalls
	opts.L0CompactionThreshold = 4  // Start compaction at 4 L0 files (default 4)
	opts.L0StopWritesThreshold = 12 // Stall writes at 12 L0 files (default 12)
	opts.LBaseMaxBytes = 64 << 20   // 64 MiB base level (default 64 MiB)

	// Value separation for large values (our rows are up to 32KB)
	// Only enable for values > 4KB to avoid overhead on smaller values
	opts.Experimental.ValueSeparationPolicy = func() pebbledb.ValueSeparationPolicy {
		return pebbledb.ValueSeparationPolicy{
			Enabled:               true,
			MinimumSize:           4096, // Values > 4KB go to blob files
			MaxBlobReferenceDepth: 4,    // Limit overlapping blob files
			TargetGarbageRatio:    0.3,  // Rewrite when 30% garbage
			RewriteMinimumAge:     0,    // Allow immediate rewrites
		}
	}

	db, err := pebbledb.Open(cfg.Path, opts)
	if err != nil {
		return nil, fmt.Errorf("opening pebble database: %w", err)
	}

	return &Store{
		cfg: cfg,
		db:  db,
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
func (s *Store) Put(_ context.Context, promise *PaymentPromise, shard *types.BlobShard, pruneAt time.Time) error {
	batch := s.db.NewBatch()
	defer batch.Close()

	// write payment promise
	promiseProto, err := promise.ToProto()
	if err != nil {
		return fmt.Errorf("converting payment promise to proto: %w", err)
	}
	ppData, err := gogoproto.Marshal(promiseProto)
	if err != nil {
		return fmt.Errorf("marshaling payment promise: %w", err)
	}
	promiseHash, err := promise.Hash()
	if err != nil {
		return fmt.Errorf("getting promise hash: %w", err)
	}
	if err := batch.Set(promiseKey(promiseHash), ppData, pebbledb.NoSync); err != nil {
		return fmt.Errorf("putting payment promise: %w", err)
	}

	// write shard
	shardData, err := gogoproto.Marshal(shard)
	if err != nil {
		return fmt.Errorf("marshaling shard: %w", err)
	}
	if err := batch.Set(shardKey(promise.Commitment, promiseHash), shardData, pebbledb.NoSync); err != nil {
		return fmt.Errorf("putting shard: %w", err)
	}

	// write prune index
	if err := batch.Set(pruneKey(pruneAt, promise.Commitment, promiseHash), []byte{}, pebbledb.NoSync); err != nil {
		return fmt.Errorf("putting prune index: %w", err)
	}

	return batch.Commit(pebbledb.NoSync)
}

// Get retrieves [types.BlobShard] for the given [Commitment].
//
// When multiple payment promises exist for the same commitment, only the first shard is returned.
// This prevents unbounded message sizes when the same blob is uploaded multiple times.
// Underlying store's must ensure deterministic key ordering to ensure validators return shards as they were uploaded.
//
// If unmarshaling fails for some entries, it continues trying others.
// Returns an error only if all entries fail to unmarshal or if no shards are found.
func (s *Store) Get(_ context.Context, commitment Commitment) (*types.BlobShard, error) {
	prefix := fmt.Appendf(nil, "/shard/%s/", commitment.String())
	iter, err := s.db.NewIter(&pebbledb.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("creating iterator: %w", err)
	}
	defer iter.Close()

	var rerr error
	for valid := iter.First(); valid; valid = iter.Next() {
		val, err := iter.ValueAndErr()
		if err != nil {
			rerr = errors.Join(rerr, err)
			continue
		}

		shard := &types.BlobShard{}
		if err := gogoproto.Unmarshal(val, shard); err != nil {
			rerr = errors.Join(rerr, fmt.Errorf("unmarshaling shard: %w", err))
			continue
		}

		// return first valid shard found
		return shard, nil
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterating shards: %w", err)
	}
	if rerr != nil {
		return nil, rerr
	}
	return nil, ErrStoreNotFound
}

// GetPaymentPromise retrieves a [PaymentPromise] by its hash.
func (s *Store) GetPaymentPromise(_ context.Context, promiseHash []byte) (*PaymentPromise, error) {
	data, closer, err := s.db.Get(promiseKey(promiseHash))
	if err != nil {
		return nil, fmt.Errorf("getting payment promise: %w", err)
	}
	defer closer.Close()

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
func (s *Store) PruneBefore(_ context.Context, before time.Time) (int, error) {
	prefix := []byte("/prune/")
	iter, err := s.db.NewIter(&pebbledb.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return 0, fmt.Errorf("creating iterator: %w", err)
	}
	defer iter.Close()

	batch := s.db.NewBatch()
	defer batch.Close()

	pruned := 0
	beforeStr := formatTimestamp(before.UTC())
	for valid := iter.First(); valid; valid = iter.Next() {
		key := iter.Key()

		// extract timestamp from key: /prune/YYYYMMDDHHmm/...
		// early termination: keys are sorted, so if timestamp >= cutoff, we're done
		keyStr := string(key)
		timestampStr := keyStr[7:19] // skip "/prune/" and take 12 chars
		if timestampStr >= beforeStr {
			break
		}

		// parse key: /prune/<timestamp>/<commitment>/<promise-hash>
		commitment, promiseHash, ok := parsePruneKey(keyStr)
		if !ok {
			continue
		}

		// delete all related entries
		if err := batch.Delete(key, pebbledb.NoSync); err != nil {
			return pruned, fmt.Errorf("deleting prune index: %w", err)
		}
		if err := batch.Delete(shardKey(commitment, promiseHash), pebbledb.NoSync); err != nil {
			return pruned, fmt.Errorf("deleting shard: %w", err)
		}
		if err := batch.Delete(promiseKey(promiseHash), pebbledb.NoSync); err != nil {
			return pruned, fmt.Errorf("deleting payment promise: %w", err)
		}
		pruned++
	}

	if err := iter.Error(); err != nil {
		return pruned, fmt.Errorf("iterating prune index: %w", err)
	}
	if err := batch.Commit(pebbledb.NoSync); err != nil {
		return pruned, fmt.Errorf("committing batch: %w", err)
	}
	return pruned, nil
}

// Close closes the underlying Pebble database.
func (s *Store) Close() error {
	return s.db.Close()
}

// formatTimestamp formats a timestamp with minute precision (YYYYMMDDHHmm).
// This format is used for timestamp-based indexing in the datastore.
func formatTimestamp(timestamp time.Time) string {
	return timestamp.Format("200601021504")
}

func promiseKey(promiseHash []byte) []byte {
	return fmt.Appendf(nil, "/pp/%s", hex.EncodeToString(promiseHash))
}

func shardKey(commitment Commitment, promiseHash []byte) []byte {
	return fmt.Appendf(nil, "/shard/%s/%s", commitment.String(), hex.EncodeToString(promiseHash))
}

func pruneKey(pruneAt time.Time, commitment Commitment, promiseHash []byte) []byte {
	return []byte(fmt.Sprintf("/prune/%s/%s/%s", formatTimestamp(pruneAt.UTC()), commitment.String(), hex.EncodeToString(promiseHash)))
}

// prefixUpperBound returns the upper bound for a prefix scan.
// It increments the last byte of the prefix to create an exclusive upper bound.
// For example, "/shard/abc" returns "/shard/abd".
func prefixUpperBound(prefix []byte) []byte {
	upper := make([]byte, len(prefix))
	copy(upper, prefix)
	for i := len(upper) - 1; i >= 0; i-- {
		upper[i]++
		if upper[i] != 0 {
			return upper
		}
	}
	// all 0xff bytes - return nil to indicate no upper bound
	return nil
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
