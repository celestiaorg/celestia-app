package fibre

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	gogoproto "github.com/cosmos/gogoproto/proto"
	ds "github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	dssync "github.com/ipfs/go-datastore/sync"
	badger "github.com/ipfs/go-ds-badger4"
	pebble "github.com/ipfs/go-ds-pebble"
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

// blobSaver abstracts how batched key-value entries are persisted.
// Implementations control the commit strategy: immediate or coalesced.
type blobSaver interface {
	submit(ctx context.Context, entries []batchEntry) error
	close()
}

// Store manages persistent storage of [PaymentPromise] and row data.
// It provides indexed access by [Commitment], promise hash, and timestamp.
//
// Writes are dispatched through a [blobSaver]. The default implementation ([writeBatcher])
// coalesces concurrent [Put] calls into a single batch commit, amortizing the commit cost.
type Store struct {
	cfg   StoreConfig
	ds    ds.Batching
	saver blobSaver
}

// NewMemoryStore creates a new [Store] with an in-memory datastore.
func NewMemoryStore(cfg StoreConfig) *Store {
	memDS := dssync.MutexWrap(ds.NewMapDatastore())
	return &Store{
		cfg:   cfg,
		ds:    memDS,
		saver: newWriteBatcher(memDS),
	}
}

// NewBadgerStore creates a new [Store] with a badger4 datastore at the given path.
// Tuned for FIBRE's use case: large values (32KB rows), bulk writes/reads.
func NewBadgerStore(cfg StoreConfig) (*Store, error) {
	opts := badger.DefaultOptions

	// Value log settings - optimized for large values (32KB rows)
	opts.ValueThreshold = 1024 // Values > 1KB go to value log (default 1MB is too high)

	// Compaction settings - reduce write stalls during bulk writes
	opts.NumMemtables = 5             // More memtables before stall (default 5)
	opts.NumLevelZeroTables = 5       // L0 tables before compaction starts (default 5)
	opts.NumLevelZeroTablesStall = 15 // L0 tables before write stall (default 15)
	opts.NumCompactors = 4            // Parallel compaction goroutines (default 4)

	// GC settings - for time-based pruning workload
	opts.GcDiscardRatio = 0.2
	opts.GcSleep = time.Second
	opts.GcInterval = time.Minute

	bds, err := badger.NewDatastore(cfg.Path, &opts)
	if err != nil {
		return nil, fmt.Errorf("creating badger datastore: %w", err)
	}

	return &Store{
		cfg:   cfg,
		ds:    bds,
		saver: newWriteBatcher(bds),
	}, nil
}

// NewPebbleStore creates a new [Store] with a pebble datastore at the given path.
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

	pds, err := pebble.NewDatastore(cfg.Path, pebble.WithPebbleOpts(opts))
	if err != nil {
		return nil, fmt.Errorf("creating pebble datastore: %w", err)
	}

	return &Store{
		cfg:   cfg,
		ds:    pds,
		saver: newWriteBatcher(pds),
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
//
// Serialization happens in the caller's goroutine, but the actual write is submitted to the
// [writeBatcher] which coalesces concurrent writes into a single batch commit.
func (s *Store) Put(ctx context.Context, promise *PaymentPromise, shard *types.BlobShard, pruneAt time.Time) error {
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

	shardData, err := gogoproto.Marshal(shard)
	if err != nil {
		return fmt.Errorf("marshaling shard: %w", err)
	}

	return s.saver.submit(ctx, []batchEntry{
		{key: promiseKey(promiseHash), value: ppData},
		{key: shardKey(promise.Commitment, promiseHash), value: shardData},
		{key: pruneKey(pruneAt, promise.Commitment, promiseHash), value: []byte{}},
	})
}

// Get retrieves [types.BlobShard] for the given [Commitment].
//
// When multiple payment promises exist for the same commitment, only the first shard is returned.
// This prevents unbounded message sizes when the same blob is uploaded multiple times.
// Underlying store's must ensure deterministic key ordering to ensure validators return shards as they were uploaded.
//
// If unmarshaling fails for some entries, it continues trying others.
// Returns an error only if all entries fail to unmarshal or if no shards are found.
func (s *Store) Get(ctx context.Context, commitment Commitment) (*types.BlobShard, error) {
	results, err := s.ds.Query(ctx, query.Query{
		Prefix: fmt.Sprintf("/shard/%s", commitment.String()),
	})
	if err != nil {
		return nil, fmt.Errorf("querying shards: %w", err)
	}
	defer results.Close()

	var rerr error
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

		// return first valid shard found
		return shard, nil
	}

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
	beforeStr := formatTimestamp(before.UTC())
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

// Close stops the blob saver and closes the underlying datastore.
func (s *Store) Close() error {
	s.saver.close()
	return s.ds.Close()
}

// batchEntry is a single key-value pair to be written to the datastore.
type batchEntry struct {
	key   ds.Key
	value []byte
}

// directWriter implements [blobSaver] by committing each submit call immediately
// in its own batch. This is the simplest strategy with no write coalescing.
type directWriter struct {
	ds ds.Batching
}

func newDirectWriter(store ds.Batching) *directWriter {
	return &directWriter{ds: store}
}

func (dw *directWriter) submit(ctx context.Context, entries []batchEntry) error {
	batch, err := dw.ds.Batch(ctx)
	if err != nil {
		return fmt.Errorf("creating batch: %w", err)
	}
	for _, e := range entries {
		if err := batch.Put(ctx, e.key, e.value); err != nil {
			return fmt.Errorf("putting entry: %w", err)
		}
	}
	return batch.Commit(ctx)
}

func (dw *directWriter) close() {}

// writeBatcher implements [blobSaver] by coalescing multiple write operations into
// a single batch commit, amortizing the commit cost across all writes in the batch.
//
// Under concurrent load (e.g. 4,650 simultaneous UploadShard RPCs), each Put would
// otherwise create its own batch and Commit independently. The batcher collects
// pending writes and commits them in a single batch, paying the commit cost once
// for N writes instead of N times.

type writeRequest struct {
	entries []batchEntry
	result  chan error
}
type writeBatcher struct {
	ds         ds.Batching
	requests   chan *writeRequest
	done       chan struct{}
	maxPending int
}

const (
	defaultWriteBatcherQueueSize  = 4096
	defaultWriteBatcherMaxPending = 512
)

func newWriteBatcher(store ds.Batching) *writeBatcher {
	return newWriteBatcherWithOpts(
		store,
		defaultWriteBatcherQueueSize,
		defaultWriteBatcherMaxPending,
	)
}

func newWriteBatcherWithOpts(store ds.Batching, queueSize, maxPending int) *writeBatcher {
	wb := &writeBatcher{
		ds:         store,
		requests:   make(chan *writeRequest, queueSize),
		done:       make(chan struct{}),
		maxPending: maxPending,
	}
	go wb.run()
	return wb
}

func (wb *writeBatcher) run() {
	defer close(wb.done)

	for {
		first, ok := <-wb.requests
		if !ok {
			return
		}

		pending := make([]*writeRequest, 1, wb.maxPending)
		pending[0] = first
		pending = wb.drain(pending)

		err := wb.commitAll(pending)
		for _, req := range pending {
			req.result <- err
		}
	}
}

// drain collects all currently queued requests without waiting.
func (wb *writeBatcher) drain(pending []*writeRequest) []*writeRequest {
	for len(pending) < wb.maxPending {
		select {
		case req, ok := <-wb.requests:
			if !ok {
				return pending
			}
			pending = append(pending, req)
		default:
			return pending
		}
	}
	return pending
}

func (wb *writeBatcher) commitAll(requests []*writeRequest) error {
	ctx := context.Background()
	batch, err := wb.ds.Batch(ctx)
	if err != nil {
		return fmt.Errorf("creating batch: %w", err)
	}

	for _, req := range requests {
		for _, entry := range req.entries {
			if err := batch.Put(ctx, entry.key, entry.value); err != nil {
				return fmt.Errorf("adding to batch: %w", err)
			}
		}
	}

	return batch.Commit(ctx)
}

// submit sends a write request to the batcher and blocks until the coalesced
// batch containing this request has been committed.
func (wb *writeBatcher) submit(ctx context.Context, entries []batchEntry) error {
	req := &writeRequest{
		entries: entries,
		result:  make(chan error, 1),
	}

	select {
	case wb.requests <- req:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-req.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (wb *writeBatcher) close() {
	close(wb.requests)
	<-wb.done
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
	return ds.NewKey(fmt.Sprintf("/prune/%s/%s/%s", formatTimestamp(pruneAt.UTC()), commitment.String(), hex.EncodeToString(promiseHash)))
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
