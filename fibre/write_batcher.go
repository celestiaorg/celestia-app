package fibre

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	ds "github.com/ipfs/go-datastore"
	pebble "github.com/ipfs/go-ds-pebble"
	"golang.org/x/sync/errgroup"
)

// putter abstracts how prepared write payloads are persisted.
// Implementations control the commit strategy: immediate or coalesced.
type putter interface {
	submit(ctx context.Context, payload *putPayload) error
	close()
}

type planCommitter interface {
	commit(ctx context.Context, puts []*encodedPut) error
}

type genericPlanCommitter struct {
	store ds.Batching
}

type pebblePlanCommitter struct {
	store *pebble.Datastore
}

type putPayload struct {
	promiseProto *types.PaymentPromise
	promiseHash  []byte
	commitment   Commitment
	shard        *types.BlobShard
	pruneAt      time.Time
	ppSize       int
	shardSize    int
}

type encodedPut struct {
	promiseKey   string
	shardKey     string
	pruneKey     string
	ppData       []byte
	shardData    []byte
	ppPool       *sync.Pool
	shardPool    *sync.Pool
	ppPoolCap    int
	shardPoolCap int
	bytes        int
}

func preparePut(promise *PaymentPromise, shard *types.BlobShard, pruneAt time.Time) (*putPayload, error) {
	promiseProto, err := promise.ToProto()
	if err != nil {
		return nil, fmt.Errorf("converting payment promise to proto: %w", err)
	}
	promiseHash, err := promise.Hash()
	if err != nil {
		return nil, fmt.Errorf("getting promise hash: %w", err)
	}
	return &putPayload{
		promiseProto: promiseProto,
		promiseHash:  promiseHash,
		commitment:   promise.Commitment,
		shard:        shard,
		pruneAt:      pruneAt,
		ppSize:       promiseProto.Size(),
		shardSize:    shard.Size(),
	}, nil
}

func (p *putPayload) batchBytes() int {
	return promiseKeyLen(p.promiseHash) + p.ppSize +
		shardKeyLen(p.promiseHash) + p.shardSize +
		pruneKeyLen(p.promiseHash)
}

func (p *putPayload) encode(syncPoolProvider *syncPoolProvider) (*encodedPut, error) {
	ppPool, ppPoolCap := syncPoolProvider.poolForSize(p.ppSize)
	ppData, err := marshalSized(p.promiseProto, ppPool, ppPoolCap)
	if err != nil {
		return nil, fmt.Errorf("marshaling payment promise: %w", err)
	}

	shardPool, shardPoolCap := syncPoolProvider.poolForSize(p.shardSize)
	shardData, err := marshalSized(p.shard, shardPool, shardPoolCap)
	if err != nil {
		putMarshalBuf(ppPool, ppPoolCap, ppData)
		return nil, fmt.Errorf("marshaling shard: %w", err)
	}

	put := &encodedPut{
		promiseKey:   promiseKeyString(p.promiseHash),
		shardKey:     shardKeyString(p.commitment, p.promiseHash),
		pruneKey:     pruneKeyString(p.pruneAt, p.commitment, p.promiseHash),
		ppData:       ppData,
		shardData:    shardData,
		ppPool:       ppPool,
		shardPool:    shardPool,
		ppPoolCap:    ppPoolCap,
		shardPoolCap: shardPoolCap,
	}
	put.bytes = len(put.promiseKey) + len(put.ppData) + len(put.shardKey) + len(put.shardData) + len(put.pruneKey)
	return put, nil
}

func (p *encodedPut) release() {
	if len(p.ppData) > 0 {
		putMarshalBuf(p.ppPool, p.ppPoolCap, p.ppData)
	}
	if len(p.shardData) > 0 {
		putMarshalBuf(p.shardPool, p.shardPoolCap, p.shardData)
	}
	p.ppData = nil
	p.shardData = nil
	p.ppPool = nil
	p.shardPool = nil
	p.ppPoolCap = 0
	p.shardPoolCap = 0
}

func (p *encodedPut) applyGeneric(ctx context.Context, batch ds.Batch) error {
	if err := batch.Put(ctx, ds.RawKey(p.promiseKey), p.ppData); err != nil {
		return fmt.Errorf("putting payment promise: %w", err)
	}
	if err := batch.Put(ctx, ds.RawKey(p.shardKey), p.shardData); err != nil {
		return fmt.Errorf("putting shard: %w", err)
	}
	if err := batch.Put(ctx, ds.RawKey(p.pruneKey), nil); err != nil {
		return fmt.Errorf("putting prune index: %w", err)
	}
	return nil
}

func (p *encodedPut) applyPebble(batch *pebbledb.Batch) error {
	if err := writePebbleEntry(batch, p.promiseKey, p.ppData); err != nil {
		return fmt.Errorf("writing payment promise: %w", err)
	}
	if err := writePebbleEntry(batch, p.shardKey, p.shardData); err != nil {
		return fmt.Errorf("writing shard: %w", err)
	}
	if err := writePebbleEntry(batch, p.pruneKey, nil); err != nil {
		return fmt.Errorf("writing prune index: %w", err)
	}
	return nil
}

func defaultPlanCommitter(store ds.Batching) planCommitter {
	if pds, ok := store.(*pebble.Datastore); ok {
		return pebblePlanCommitter{store: pds}
	}
	return genericPlanCommitter{store: store}
}

func releaseEncodedPuts(puts []*encodedPut) {
	for _, put := range puts {
		if put != nil {
			put.release()
		}
	}
}

func (c genericPlanCommitter) commit(ctx context.Context, puts []*encodedPut) error {
	defer releaseEncodedPuts(puts)

	batch, err := c.store.Batch(ctx)
	if err != nil {
		return fmt.Errorf("creating batch: %w", err)
	}
	for _, put := range puts {
		if err := put.applyGeneric(ctx, batch); err != nil {
			return err
		}
	}
	if err := batch.Commit(ctx); err != nil {
		return fmt.Errorf("committing batch: %w", err)
	}
	return nil
}

func (c pebblePlanCommitter) commit(_ context.Context, puts []*encodedPut) error {
	defer releaseEncodedPuts(puts)

	batch := c.store.DB.NewBatchWithSize(pebbleEncodedPutsBatchSize(puts))
	defer batch.Close()

	for _, put := range puts {
		if err := put.applyPebble(batch); err != nil {
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
	committer        planCommitter
	syncPoolProvider *syncPoolProvider
}

func newDirectPutter(store ds.Batching) *directPutter {
	return &directPutter{
		committer:        defaultPlanCommitter(store),
		syncPoolProvider: newSyncPoolProvider(defaultSyncPoolClasses),
	}
}

func (dw *directPutter) submit(ctx context.Context, payload *putPayload) error {
	put, err := payload.encode(dw.syncPoolProvider)
	if err != nil {
		return err
	}
	return dw.committer.commit(ctx, []*encodedPut{put})
}

func (dw *directPutter) close() {}

// writeBatcher runs a three-stage pipeline:
// 1. callers submit logical put plans,
// 2. encoder workers marshal plans into pooled byte buffers in parallel,
// 3. a single commit loop coalesces encoded puts into shared datastore batches.
type writeRequest struct {
	payload *putPayload
	result  chan error
}

var writeRequestPool = sync.Pool{
	New: func() any {
		return &writeRequest{result: make(chan error, 1)}
	},
}

type writeBatcher struct {
	committer        planCommitter
	syncPoolProvider *syncPoolProvider
	requests         chan *writeRequest
	done             chan struct{}
	submitters       submitGate
	encoderWorkers   int
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
	pebbleBatchHeader             = 12
)

type writeBatcherOptions struct {
	encoderWorkers   int
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
	if opts.encoderWorkers <= 0 {
		opts.encoderWorkers = runtime.GOMAXPROCS(0)
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
		committer:        defaultPlanCommitter(store),
		syncPoolProvider: newSyncPoolProvider(defaultSyncPoolClasses),
		requests:         make(chan *writeRequest, opts.queueSize),
		done:             make(chan struct{}),
		submitters: submitGate{
			drained: make(chan struct{}),
		},
		encoderWorkers:   opts.encoderWorkers,
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
		pendingBytes := first.payload.batchBytes()

		pending, pendingBytes = wb.drain(pending, pendingBytes, nil)

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
				pendingBytes += req.payload.batchBytes()
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
			pendingBytes += req.payload.batchBytes()
		case <-timer.C:
			return pending, pendingBytes
		}
	}
	return pending, pendingBytes
}

func (wb *writeBatcher) commitAll(ctx context.Context, requests []*writeRequest) error {
	puts, err := wb.encodeAll(requests)
	if err != nil {
		return err
	}
	return wb.committer.commit(ctx, puts)
}

func (wb *writeBatcher) encodeAll(requests []*writeRequest) ([]*encodedPut, error) {
	if len(requests) == 0 {
		return nil, nil
	}
	if len(requests) == 1 || wb.encoderWorkers <= 1 {
		put, err := requests[0].payload.encode(wb.syncPoolProvider)
		requests[0].payload = nil
		if err != nil {
			return nil, err
		}
		return []*encodedPut{put}, nil
	}

	puts := make([]*encodedPut, len(requests))
	jobs := make(chan int, len(requests))
	g, ctx := errgroup.WithContext(context.Background())

	workers := min(wb.encoderWorkers, len(requests))
	for i := 0; i < workers; i++ {
		g.Go(func() error {
			for idx := range jobs {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				put, err := requests[idx].payload.encode(wb.syncPoolProvider)
				requests[idx].payload = nil
				if err != nil {
					return err
				}
				puts[idx] = put
			}
			return nil
		})
	}

	for i := range requests {
		jobs <- i
	}
	close(jobs)

	if err := g.Wait(); err != nil {
		releaseEncodedPuts(puts)
		return nil, err
	}
	return puts, nil
}

func (wb *writeBatcher) submit(ctx context.Context, payload *putPayload) error {
	if payload == nil {
		return nil
	}

	req := writeRequestPool.Get().(*writeRequest)
	req.payload = payload

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
	req.payload = nil
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

var defaultSyncPoolClasses = []int{
	1 << 10,
	4 << 10,
	16 << 10,
	64 << 10,
	256 << 10,
	1 << 20,
	4 << 20,
	16 << 20,
	32 << 20,
}

func marshalSized(m sizedMarshaler, pool *sync.Pool, poolCap int) ([]byte, error) {
	size := m.Size()
	var buf []byte
	if pool == nil {
		buf = make([]byte, size)
	} else {
		buf = pool.Get().([]byte)[:size]
	}
	n, err := m.MarshalToSizedBuffer(buf)
	if err != nil {
		putMarshalBuf(pool, poolCap, buf)
		return nil, err
	}
	return buf[:n], nil
}

type syncPoolProvider struct {
	caps  []int
	pools []sync.Pool
}

func newSyncPoolProvider(caps []int) *syncPoolProvider {
	pools := make([]sync.Pool, len(caps))
	for i, classCap := range caps {
		classCap := classCap
		pools[i] = sync.Pool{
			New: func() any {
				return make([]byte, classCap)
			},
		}
	}
	return &syncPoolProvider{
		caps:  caps,
		pools: pools,
	}
}

func (p *syncPoolProvider) poolForSize(size int) (*sync.Pool, int) {
	idx := p.classIndex(size)
	if idx < 0 {
		return nil, size
	}
	return &p.pools[idx], p.caps[idx]
}

func putMarshalBuf(pool *sync.Pool, poolCap int, buf []byte) {
	if pool == nil || poolCap == 0 || cap(buf) != poolCap {
		return
	}
	pool.Put(buf[:poolCap])
}

func (p *syncPoolProvider) classIndex(size int) int {
	for i, classCap := range p.caps {
		if size <= classCap {
			return i
		}
	}
	return -1
}

func pebbleEncodedPutsBatchSize(puts []*encodedPut) int {
	size := pebbleBatchHeader
	for _, put := range puts {
		size += pebbleEncodedPutBatchSize(put)
	}
	return size
}

func pebbleEncodedPutBatchSize(put *encodedPut) int {
	return pebbleBatchEntrySize(len(put.promiseKey), len(put.ppData)) +
		pebbleBatchEntrySize(len(put.shardKey), len(put.shardData)) +
		pebbleBatchEntrySize(len(put.pruneKey), 0)
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

func promiseKeyString(promiseHash []byte) string {
	return promiseKeyPrefix + hex.EncodeToString(promiseHash)
}

func shardKeyString(commitment Commitment, promiseHash []byte) string {
	return shardKeyPrefix + commitment.String() + "/" + hex.EncodeToString(promiseHash)
}

func pruneKeyString(pruneAt time.Time, commitment Commitment, promiseHash []byte) string {
	return pruneKeyPrefix + formatTimestamp(pruneAt.UTC()) + "/" + commitment.String() + "/" + hex.EncodeToString(promiseHash)
}

func writePebbleEntry(batch *pebbledb.Batch, key string, value []byte) error {
	op := batch.SetDeferred(len(key), len(value))
	copy(op.Key, key)
	copy(op.Value, value)
	if err := op.Finish(); err != nil {
		return fmt.Errorf("finishing pebble batch op: %w", err)
	}
	return nil
}

func promiseKey(promiseHash []byte) ds.Key {
	return ds.RawKey(promiseKeyString(promiseHash))
}

func shardKey(commitment Commitment, promiseHash []byte) ds.Key {
	return ds.RawKey(shardKeyString(commitment, promiseHash))
}

func pruneKey(pruneAt time.Time, commitment Commitment, promiseHash []byte) ds.Key {
	return ds.RawKey(pruneKeyString(pruneAt, commitment, promiseHash))
}
