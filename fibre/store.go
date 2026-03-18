package fibre

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
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

// ErrStoreClosed is returned when a write is attempted after the store has started closing.
var ErrStoreClosed = errors.New("store closed")

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

// putter abstracts how prepared write plans are persisted.
// Implementations control the commit strategy: immediate or coalesced.
type putter interface {
	submit(ctx context.Context, plan *putPlan) error
	close()
}

// Store manages persistent storage of [PaymentPromise] and row data.
// It provides indexed access by [Commitment], promise hash, and timestamp.
//
// Writes are dispatched through a [putter]. The default implementation ([writeBatcher])
// coalesces concurrent [Put] calls into a single commit, amortizing the commit cost.
type Store struct {
	cfg    StoreConfig
	ds     ds.Batching
	putter putter
}

// NewMemoryStore creates a new [Store] with an in-memory datastore.
func NewMemoryStore(cfg StoreConfig) *Store {
	memDS := dssync.MutexWrap(ds.NewMapDatastore())
	return newStore(cfg, memDS)
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

	return newStore(cfg, bds), nil
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

	return newStore(cfg, pds), nil
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
// Write preparation happens in the caller's goroutine. Execution is delegated to the configured
// [putter], which may commit immediately or coalesce multiple prepared puts.
func (s *Store) Put(ctx context.Context, promise *PaymentPromise, shard *types.BlobShard, pruneAt time.Time) error {
	plan, err := preparePut(promise, shard, pruneAt)
	if err != nil {
		return err
	}
	return s.putter.submit(ctx, plan)
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
		Prefix: "/shard/" + commitment.String(),
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

// Close stops the putter and closes the underlying datastore.
func (s *Store) Close() error {
	s.putter.close()
	return s.ds.Close()
}

func newStore(cfg StoreConfig, store ds.Batching) *Store {
	return &Store{
		cfg:    cfg,
		ds:     store,
		putter: newWriteBatcher(store),
	}
}

type planCommitter interface {
	commit(ctx context.Context, plans []*putPlan) error
}

type genericPlanCommitter struct {
	store ds.Batching
}

type pebblePlanCommitter struct {
	store *pebble.Datastore
}

type putPlan struct {
	promiseProto *types.PaymentPromise
	promiseHash  []byte
	commitment   Commitment
	shard        *types.BlobShard
	pruneAt      time.Time
	ppSize       int
	shardSize    int
}

func preparePut(promise *PaymentPromise, shard *types.BlobShard, pruneAt time.Time) (*putPlan, error) {
	promiseProto, err := promise.ToProto()
	if err != nil {
		return nil, fmt.Errorf("converting payment promise to proto: %w", err)
	}
	promiseHash, err := promise.Hash()
	if err != nil {
		return nil, fmt.Errorf("getting promise hash: %w", err)
	}
	return &putPlan{
		promiseProto: promiseProto,
		promiseHash:  promiseHash,
		commitment:   promise.Commitment,
		shard:        shard,
		pruneAt:      pruneAt,
		ppSize:       promiseProto.Size(),
		shardSize:    shard.Size(),
	}, nil
}

func (p *putPlan) batchBytes() int {
	return promiseKeyLen(p.promiseHash) + p.ppSize +
		shardKeyLen(p.promiseHash) + p.shardSize +
		pruneKeyLen(p.promiseHash)
}

func (p *putPlan) applyGeneric(ctx context.Context, batch ds.Batch) error {
	ppData, err := gogoproto.Marshal(p.promiseProto)
	if err != nil {
		return fmt.Errorf("marshaling payment promise: %w", err)
	}
	if err := batch.Put(ctx, promiseKey(p.promiseHash), ppData); err != nil {
		return fmt.Errorf("putting payment promise: %w", err)
	}

	shardData, err := gogoproto.Marshal(p.shard)
	if err != nil {
		return fmt.Errorf("marshaling shard: %w", err)
	}
	if err := batch.Put(ctx, shardKey(p.commitment, p.promiseHash), shardData); err != nil {
		return fmt.Errorf("putting shard: %w", err)
	}

	if err := batch.Put(ctx, pruneKey(p.pruneAt, p.commitment, p.promiseHash), nil); err != nil {
		return fmt.Errorf("putting prune index: %w", err)
	}
	return nil
}

func (p *putPlan) applyPebble(batch *pebbledb.Batch) error {
	if err := writePebblePaymentPromise(batch, p); err != nil {
		return err
	}
	if err := writePebbleShard(batch, p); err != nil {
		return err
	}
	if err := writePebblePruneIndex(batch, p); err != nil {
		return err
	}
	return nil
}

func defaultPlanCommitter(store ds.Batching) planCommitter {
	if pds, ok := store.(*pebble.Datastore); ok {
		return pebblePlanCommitter{store: pds}
	}
	return genericPlanCommitter{store: store}
}

func (c genericPlanCommitter) commit(ctx context.Context, plans []*putPlan) error {
	batch, err := c.store.Batch(ctx)
	if err != nil {
		return fmt.Errorf("creating batch: %w", err)
	}
	for _, plan := range plans {
		if err := plan.applyGeneric(ctx, batch); err != nil {
			return err
		}
	}
	if err := batch.Commit(ctx); err != nil {
		return fmt.Errorf("committing batch: %w", err)
	}
	return nil
}

func (c pebblePlanCommitter) commit(_ context.Context, plans []*putPlan) error {
	batch := c.store.DB.NewBatchWithSize(pebblePlansBatchSize(plans))
	defer batch.Close()

	for _, plan := range plans {
		if err := plan.applyPebble(batch); err != nil {
			return err
		}
	}

	if err := batch.Commit(pebbledb.NoSync); err != nil {
		return fmt.Errorf("committing pebble batch: %w", err)
	}
	return nil
}

// directPutter commits each prepared write immediately. It exists as a baseline
// and shares the same commit path as the batcher.
type directPutter struct {
	committer planCommitter
}

func newDirectPutter(store ds.Batching) *directPutter {
	return &directPutter{committer: defaultPlanCommitter(store)}
}

func (dw *directPutter) submit(ctx context.Context, plan *putPlan) error {
	return dw.committer.commit(ctx, []*putPlan{plan})
}

func (dw *directPutter) close() {}

// writeBatcher coalesces multiple prepared puts into a single commit. It does
// not introduce a second write architecture: it queues [putPlan]s and flushes
// them through the same backend-specific committers used by direct writes.
type writeRequest struct {
	plan   *putPlan
	bytes  int
	result chan error
}

var writeRequestPool = sync.Pool{
	New: func() any {
		return &writeRequest{result: make(chan error, 1)}
	},
}

type writeBatcher struct {
	committer        planCommitter
	requests         chan *writeRequest
	done             chan struct{}
	submitters       submitGate
	maxPending       int
	minPending       int
	minBatchBytes    int
	targetBatchBytes int
	flushInterval    time.Duration
}

type submitGate struct {
	drained   chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	active    int
	closed    bool
}

const (
	defaultWriteBatcherQueueSize  = 4096
	defaultWriteBatcherMaxPending = 512
	defaultWriteBatcherMinPending = 64
	defaultWriteBatcherMinBytes   = 64 << 20
	defaultWriteBatcherTargetSize = 1 << 30
	defaultWriteBatcherFlushDelay = 1 * time.Millisecond
)

type writeBatcherOptions struct {
	queueSize        int
	minPending       int
	maxPending       int
	minBatchBytes    int
	targetBatchBytes int
	flushInterval    time.Duration
}

func newWriteBatcher(store ds.Batching) *writeBatcher {
	return newWriteBatcherWithOpts(
		store,
		writeBatcherOptions{
			queueSize:        defaultWriteBatcherQueueSize,
			minPending:       defaultWriteBatcherMinPending,
			maxPending:       defaultWriteBatcherMaxPending,
			minBatchBytes:    defaultWriteBatcherMinBytes,
			targetBatchBytes: defaultWriteBatcherTargetSize,
			flushInterval:    defaultWriteBatcherFlushDelay,
		},
	)
}

func newWriteBatcherWithOpts(store ds.Batching, opts writeBatcherOptions) *writeBatcher {
	if opts.queueSize <= 0 {
		opts.queueSize = defaultWriteBatcherQueueSize
	}
	if opts.minPending <= 0 {
		opts.minPending = defaultWriteBatcherMinPending
	}
	if opts.maxPending < opts.minPending {
		opts.maxPending = max(opts.minPending, defaultWriteBatcherMaxPending)
	}
	if opts.minBatchBytes <= 0 {
		opts.minBatchBytes = defaultWriteBatcherMinBytes
	}
	if opts.targetBatchBytes < opts.minBatchBytes {
		opts.targetBatchBytes = max(opts.minBatchBytes, defaultWriteBatcherTargetSize)
	}
	if opts.flushInterval <= 0 {
		opts.flushInterval = defaultWriteBatcherFlushDelay
	}

	wb := &writeBatcher{
		committer: defaultPlanCommitter(store),
		requests:  make(chan *writeRequest, opts.queueSize),
		done:      make(chan struct{}),
		submitters: submitGate{
			drained: make(chan struct{}),
		},
		maxPending:       opts.maxPending,
		minPending:       opts.minPending,
		minBatchBytes:    opts.minBatchBytes,
		targetBatchBytes: opts.targetBatchBytes,
		flushInterval:    opts.flushInterval,
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
		pendingBytes := first.bytes

		// Immediate drain (no waiting)
		pending, pendingBytes = wb.drain(pending, pendingBytes, nil)

		// If the batch is still light both in request count and total bytes,
		// briefly wait for more work. Large requests flush immediately.
		if wb.shouldWaitForMore(len(pending), pendingBytes) {
			timer := time.NewTimer(wb.flushDelayFor(len(pending), pendingBytes))
			pending, pendingBytes = wb.drain(pending, pendingBytes, timer)

			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}

		err := wb.commitAll(context.Background(), pending)
		for _, req := range pending {
			req.result <- err
		}
	}
}

func (wb *writeBatcher) shouldWaitForMore(pendingCount, pendingBytes int) bool {
	return pendingCount < wb.minPendingFor(pendingCount, pendingBytes) &&
		pendingBytes < wb.minBatchBytes &&
		pendingBytes < wb.targetBatchBytes
}

func (wb *writeBatcher) minPendingFor(pendingCount, pendingBytes int) int {
	if pendingCount == 0 {
		return wb.minPending
	}
	avgRequestBytes := pendingBytes / pendingCount
	if avgRequestBytes >= 4<<20 {
		return min(wb.minPending, 64)
	}
	return wb.minPending
}

func (wb *writeBatcher) flushDelayFor(pendingCount, pendingBytes int) time.Duration {
	if pendingCount == 0 {
		return wb.flushInterval
	}
	avgRequestBytes := pendingBytes / pendingCount
	if avgRequestBytes <= 2<<20 {
		return 2 * wb.flushInterval
	}
	return wb.flushInterval
}

// drain collects requests until:
// - maxPending reached
// - targetBatchBytes reached
// - no immediate items (if timer == nil)
// - timer fires
// - channel closed
func (wb *writeBatcher) drain(
	pending []*writeRequest,
	pendingBytes int,
	timer *time.Timer,
) ([]*writeRequest, int) {
	for len(pending) < wb.maxPending {
		if timer == nil {
			select {
			case req, ok := <-wb.requests:
				if !ok {
					return pending, pendingBytes
				}
				pending = append(pending, req)
				pendingBytes += req.bytes
			default:
				return pending, pendingBytes
			}
			continue
		}

		select {
		case req, ok := <-wb.requests:
			if !ok {
				return pending, pendingBytes
			}
			pending = append(pending, req)
			pendingBytes += req.bytes
		case <-timer.C:
			return pending, pendingBytes
		}
	}
	return pending, pendingBytes
}

func (wb *writeBatcher) commitAll(ctx context.Context, requests []*writeRequest) error {
	plans := make([]*putPlan, len(requests))
	for i, req := range requests {
		plans[i] = req.plan
	}
	return wb.committer.commit(ctx, plans)
}

func (wb *writeBatcher) submit(ctx context.Context, plan *putPlan) error {
	if plan == nil {
		return nil
	}

	req := writeRequestPool.Get().(*writeRequest)
	req.plan = plan
	req.bytes = plan.batchBytes()

	if !wb.tryAcquireSubmitter() {
		req.reset()
		writeRequestPool.Put(req)
		return ErrStoreClosed
	}
	defer wb.releaseSubmitter()

	select {
	case wb.requests <- req:
	case <-ctx.Done():
		req.reset()
		writeRequestPool.Put(req)
		return ctx.Err()
	}

	// Once a write is queued, return the actual commit result instead of a later
	// caller cancellation. This avoids reporting a failed Put after the data has
	// already been durably written by the batcher.
	err := <-req.result

	req.reset()
	writeRequestPool.Put(req)

	return err
}

func (wb *writeBatcher) close() {
	wb.submitters.closeAndWait(func() {
		close(wb.requests)
		<-wb.done
	})
}

func (req *writeRequest) reset() {
	req.plan = nil
	req.bytes = 0
}

func (wb *writeBatcher) tryAcquireSubmitter() bool {
	return wb.submitters.acquire()
}

func (wb *writeBatcher) releaseSubmitter() {
	wb.submitters.release()
}

func (g *submitGate) acquire() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed {
		return false
	}

	g.active++
	return true
}

func (g *submitGate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.active--
	if g.closed && g.active == 0 {
		close(g.drained)
	}
}

func (g *submitGate) closeAndWait(fn func()) {
	g.closeOnce.Do(func() {
		g.mu.Lock()
		g.closed = true
		hasActive := g.active != 0
		g.mu.Unlock()

		if hasActive {
			<-g.drained
		}

		fn()
	})
}

func (g *submitGate) isClosed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closed
}

type sizedMarshaler interface {
	Size() int
	MarshalToSizedBuffer([]byte) (int, error)
}

const (
	timestampLayout   = "200601021504"
	timestampLen      = len(timestampLayout)
	promiseKeyPrefix  = "/pp/"
	shardKeyPrefix    = "/shard/"
	pruneKeyPrefix    = "/prune/"
	commitmentHexLen  = CommitmentSize * 2
	pebbleBatchHeader = 12
)

func marshalToSizedBuffer(dst []byte, m sizedMarshaler) error {
	n, err := m.MarshalToSizedBuffer(dst)
	if err != nil {
		return err
	}
	if n != len(dst) {
		return fmt.Errorf("marshal size mismatch: wrote %d bytes into %d-byte buffer", n, len(dst))
	}
	return nil
}

// formatTimestamp formats a timestamp with minute precision (YYYYMMDDHHmm).
// This format is used for timestamp-based indexing in the datastore.
func formatTimestamp(timestamp time.Time) string {
	return timestamp.Format(timestampLayout)
}

func pebblePlansBatchSize(plans []*putPlan) int {
	size := pebbleBatchHeader
	for _, plan := range plans {
		size += pebblePutBatchSize(plan)
	}
	return size
}

func pebblePutBatchSize(plan *putPlan) int {
	return pebbleBatchEntrySize(promiseKeyLen(plan.promiseHash), plan.ppSize) +
		pebbleBatchEntrySize(shardKeyLen(plan.promiseHash), plan.shardSize) +
		pebbleBatchEntrySize(pruneKeyLen(plan.promiseHash), 0)
}

func pebbleBatchEntrySize(keyLen, valueLen int) int {
	return 1 + 2*binary.MaxVarintLen32 + keyLen + valueLen
}

func promiseKeyLen(promiseHash []byte) int {
	return len(promiseKeyPrefix) + hex.EncodedLen(len(promiseHash))
}

func shardKeyLen(promiseHash []byte) int {
	return len(shardKeyPrefix) + commitmentHexLen + 1 + hex.EncodedLen(len(promiseHash))
}

func pruneKeyLen(promiseHash []byte) int {
	return len(pruneKeyPrefix) + timestampLen + 1 + commitmentHexLen + 1 + hex.EncodedLen(len(promiseHash))
}

func writePebblePaymentPromise(batch *pebbledb.Batch, plan *putPlan) error {
	op := batch.SetDeferred(promiseKeyLen(plan.promiseHash), plan.ppSize)
	encodePromiseKey(op.Key, plan.promiseHash)
	if err := marshalToSizedBuffer(op.Value, plan.promiseProto); err != nil {
		return fmt.Errorf("marshaling payment promise: %w", err)
	}
	if err := op.Finish(); err != nil {
		return fmt.Errorf("finishing payment promise batch op: %w", err)
	}
	return nil
}

func writePebbleShard(batch *pebbledb.Batch, plan *putPlan) error {
	op := batch.SetDeferred(shardKeyLen(plan.promiseHash), plan.shardSize)
	encodeShardKey(op.Key, plan.commitment, plan.promiseHash)
	if err := marshalToSizedBuffer(op.Value, plan.shard); err != nil {
		return fmt.Errorf("marshaling shard: %w", err)
	}
	if err := op.Finish(); err != nil {
		return fmt.Errorf("finishing shard batch op: %w", err)
	}
	return nil
}

func writePebblePruneIndex(batch *pebbledb.Batch, plan *putPlan) error {
	op := batch.SetDeferred(pruneKeyLen(plan.promiseHash), 0)
	encodePruneKey(op.Key, plan.pruneAt, plan.commitment, plan.promiseHash)
	if err := op.Finish(); err != nil {
		return fmt.Errorf("finishing prune index batch op: %w", err)
	}
	return nil
}

func encodePromiseKey(dst []byte, promiseHash []byte) {
	pos := copy(dst, promiseKeyPrefix)
	hex.Encode(dst[pos:], promiseHash)
}

func encodeShardKey(dst []byte, commitment Commitment, promiseHash []byte) {
	pos := copy(dst, shardKeyPrefix)
	hex.Encode(dst[pos:pos+commitmentHexLen], commitment[:])
	pos += commitmentHexLen
	dst[pos] = '/'
	pos++
	hex.Encode(dst[pos:], promiseHash)
}

func encodePruneKey(dst []byte, pruneAt time.Time, commitment Commitment, promiseHash []byte) {
	pos := copy(dst, pruneKeyPrefix)
	pos = len(pruneAt.UTC().AppendFormat(dst[:pos], timestampLayout))
	dst[pos] = '/'
	pos++
	hex.Encode(dst[pos:pos+commitmentHexLen], commitment[:])
	pos += commitmentHexLen
	dst[pos] = '/'
	pos++
	hex.Encode(dst[pos:], promiseHash)
}

func promiseKey(promiseHash []byte) ds.Key {
	return ds.RawKey("/pp/" + hex.EncodeToString(promiseHash))
}

func shardKey(commitment Commitment, promiseHash []byte) ds.Key {
	return ds.RawKey("/shard/" + commitment.String() + "/" + hex.EncodeToString(promiseHash))
}

func pruneKey(pruneAt time.Time, commitment Commitment, promiseHash []byte) ds.Key {
	return ds.RawKey("/prune/" + formatTimestamp(pruneAt.UTC()) + "/" + commitment.String() + "/" + hex.EncodeToString(promiseHash))
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
