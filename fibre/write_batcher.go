package fibre

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	ds "github.com/ipfs/go-datastore"
	pebble "github.com/ipfs/go-ds-pebble"
)

// putter abstracts how prepared write payloads are persisted.
// Implementations control the commit strategy: immediate or coalesced.
type putter interface {
	submit(ctx context.Context, payload *putPayload) error
	close()
}

type payloadCommitter interface {
	commitPayloads(ctx context.Context, payloads []*putPayload) error
}

type genericPayloadCommitter struct {
	store ds.Batching
}

type pebblePayloadCommitter struct {
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

func (c genericPayloadCommitter) commitPayloads(ctx context.Context, payloads []*putPayload) error {
	batch, err := c.store.Batch(ctx)
	if err != nil {
		return fmt.Errorf("creating batch: %w", err)
	}

	for _, payload := range payloads {
		ppData, err := marshalAllocated(payload.promiseProto)
		if err != nil {
			return fmt.Errorf("marshaling payment promise: %w", err)
		}
		if err := batch.Put(ctx, promiseKey(payload.promiseHash), ppData); err != nil {
			return fmt.Errorf("putting payment promise: %w", err)
		}

		shardData, err := marshalAllocated(payload.shard)
		if err != nil {
			return fmt.Errorf("marshaling shard: %w", err)
		}
		if err := batch.Put(ctx, shardKey(payload.commitment, payload.promiseHash), shardData); err != nil {
			return fmt.Errorf("putting shard: %w", err)
		}
		if err := batch.Put(ctx, pruneKey(payload.pruneAt, payload.commitment, payload.promiseHash), nil); err != nil {
			return fmt.Errorf("putting prune index: %w", err)
		}
	}

	if err := batch.Commit(ctx); err != nil {
		return fmt.Errorf("committing batch: %w", err)
	}
	return nil
}

func (c pebblePayloadCommitter) commitPayloads(_ context.Context, payloads []*putPayload) error {
	batch := c.store.DB.NewBatchWithSize(pebblePayloadsBatchSize(payloads))
	defer batch.Close()

	for _, payload := range payloads {
		if err := payload.applyPebble(batch); err != nil {
			return err
		}
	}

	if err := batch.Commit(pebbledb.NoSync); err != nil {
		return fmt.Errorf("committing pebble batch: %w", err)
	}
	return nil
}

func defaultPayloadCommitter(store ds.Batching) payloadCommitter {
	if pds, ok := store.(*pebble.Datastore); ok {
		return pebblePayloadCommitter{store: pds}
	}
	return genericPayloadCommitter{store: store}
}

// directPutter commits each prepared write immediately. It exists as a baseline
// for benchmarks and shares the same backend-specific commit path as the batcher.
type directPutter struct {
	committer payloadCommitter
}

func newDirectPutter(store ds.Batching) *directPutter {
	return &directPutter{committer: defaultPayloadCommitter(store)}
}

func (dw *directPutter) submit(ctx context.Context, payload *putPayload) error {
	return dw.committer.commitPayloads(ctx, []*putPayload{payload})
}

func (dw *directPutter) close() {}

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
	shards        []*payloadBatcherShard
	submitters    submitGate
	shardCount    int
	maxPending    int
	minBatchBytes int
	maxBatchBytes int
	flushInterval time.Duration
}

type payloadBatcherShard struct {
	committer     payloadCommitter
	requests      chan *writeRequest
	done          chan struct{}
	maxPending    int
	minBatchBytes int
	maxBatchBytes int
	flushInterval time.Duration
	pendingBuf    []*writeRequest
	payloadBuf    []*putPayload
	reqPool       chan *writeRequest
	timer         *time.Timer
}

type submitGate struct {
	drained   chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	active    int
	closed    bool
}

const (
	defaultWriteBatcherShardCount = 4
	defaultWriteBatcherQueueSize  = 4096
	defaultWriteBatcherMaxPending = 512
	defaultWriteBatcherMinBytes   = 64 << 20
	defaultWriteBatcherMaxBytes   = 1 << 30
	defaultWriteBatcherFlushDelay = 1 * time.Millisecond
	pebbleBatchHeader             = 12
)

type writeBatcherOptions struct {
	shardCount    int
	queueSize     int
	maxPending    int
	minBatchBytes int
	maxBatchBytes int
	flushInterval time.Duration
}

func newWriteBatcher(store ds.Batching) *writeBatcher {
	return newWriteBatcherWithOpts(store, writeBatcherOptions{})
}

func newWriteBatcherWithOpts(store ds.Batching, opts writeBatcherOptions) *writeBatcher {
	if opts.shardCount <= 0 {
		opts.shardCount = defaultWriteBatcherShardCount
	}
	if opts.queueSize <= 0 {
		opts.queueSize = defaultWriteBatcherQueueSize
	}
	if opts.maxPending <= 0 {
		opts.maxPending = defaultWriteBatcherMaxPending
	}
	if opts.minBatchBytes <= 0 {
		opts.minBatchBytes = defaultWriteBatcherMinBytes
	}
	if opts.maxBatchBytes < opts.minBatchBytes {
		opts.maxBatchBytes = max(opts.minBatchBytes, defaultWriteBatcherMaxBytes)
	}
	if opts.flushInterval <= 0 {
		opts.flushInterval = defaultWriteBatcherFlushDelay
	}

	wb := &writeBatcher{
		shards: make([]*payloadBatcherShard, opts.shardCount),
		submitters: submitGate{
			drained: make(chan struct{}),
		},
		shardCount:    opts.shardCount,
		maxPending:    opts.maxPending,
		minBatchBytes: opts.minBatchBytes,
		maxBatchBytes: opts.maxBatchBytes,
		flushInterval: opts.flushInterval,
	}
	queuePerShard := max(1, opts.queueSize/opts.shardCount)
	for i := 0; i < opts.shardCount; i++ {
		wb.shards[i] = newPayloadBatcherShard(defaultPayloadCommitter(store), opts, queuePerShard)
	}
	return wb
}

func newPayloadBatcherShard(committer payloadCommitter, opts writeBatcherOptions, queueSize int) *payloadBatcherShard {
	shard := &payloadBatcherShard{
		committer:     committer,
		requests:      make(chan *writeRequest, queueSize),
		done:          make(chan struct{}),
		maxPending:    opts.maxPending,
		minBatchBytes: opts.minBatchBytes,
		maxBatchBytes: opts.maxBatchBytes,
		flushInterval: opts.flushInterval,
		pendingBuf:    make([]*writeRequest, opts.maxPending),
		payloadBuf:    make([]*putPayload, opts.maxPending),
		reqPool:       make(chan *writeRequest, opts.maxPending),
		timer:         time.NewTimer(time.Hour),
	}
	shard.stopTimer()
	go shard.run()
	return shard
}

func (wb *writeBatcher) submit(ctx context.Context, payload *putPayload) error {
	if payload == nil {
		return nil
	}

	if !wb.submitters.acquire() {
		return ErrStoreClosed
	}
	defer wb.submitters.release()

	shard := wb.shards[wb.shardIndex(payload)]
	req := shard.getRequest()
	req.payload = payload
	select {
	case shard.requests <- req:
	case <-ctx.Done():
		shard.putRequest(req)
		return ctx.Err()
	}

	err := <-req.result
	shard.putRequest(req)
	return err
}

func (wb *writeBatcher) close() {
	wb.submitters.closeAndWait(func() {
		for _, shard := range wb.shards {
			close(shard.requests)
		}
		for _, shard := range wb.shards {
			<-shard.done
		}
	})
}

func (req *writeRequest) reset() {
	req.payload = nil
}

func (wb *writeBatcher) shardIndex(payload *putPayload) int {
	if wb.shardCount <= 1 || len(payload.promiseHash) == 0 {
		return 0
	}
	var sum uint32
	for _, b := range payload.promiseHash {
		sum = sum*16777619 ^ uint32(b)
	}
	return int(sum % uint32(wb.shardCount))
}

func (shard *payloadBatcherShard) run() {
	defer shard.stopTimer()
	defer close(shard.done)

	for {
		first, ok := <-shard.requests
		if !ok {
			return
		}

		pending := shard.pendingBuf[:1]
		pending[0] = first
		pendingBytes := first.payload.batchBytes()

		pending, pendingBytes = shard.drain(pending, pendingBytes, nil)

		if shard.shouldWaitForMore(pendingBytes) {
			timerC := shard.startTimer(shard.flushDelayFor(len(pending), pendingBytes))
			pending, pendingBytes = shard.drain(pending, pendingBytes, timerC)
			shard.stopTimer()
		}

		err := shard.commitAll(context.Background(), pending)
		for _, req := range pending {
			req.result <- err
		}
		clear(pending)
	}
}

func (shard *payloadBatcherShard) shouldWaitForMore(pendingBytes int) bool {
	return pendingBytes < shard.minBatchBytes
}

func (shard *payloadBatcherShard) flushDelayFor(pendingCount, pendingBytes int) time.Duration {
	return shard.flushInterval
}

func (shard *payloadBatcherShard) drain(
	pending []*writeRequest,
	pendingBytes int,
	timerC <-chan time.Time,
) ([]*writeRequest, int) {
	for len(pending) < shard.maxPending {
		if timerC == nil {
			select {
			case req, ok := <-shard.requests:
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
		case req, ok := <-shard.requests:
			if !ok {
				return pending, pendingBytes
			}
			pending = append(pending, req)
			pendingBytes += req.payload.batchBytes()
		case <-timerC:
			return pending, pendingBytes
		}
	}
	return pending, pendingBytes
}

func (shard *payloadBatcherShard) commitAll(ctx context.Context, requests []*writeRequest) error {
	payloads := shard.payloadBuf[:len(requests)]
	for i, req := range requests {
		payloads[i] = req.payload
		req.payload = nil
	}
	err := shard.committer.commitPayloads(ctx, payloads)
	clear(payloads)
	return err
}

func (shard *payloadBatcherShard) getRequest() *writeRequest {
	select {
	case req := <-shard.reqPool:
		return req
	default:
		return writeRequestPool.Get().(*writeRequest)
	}
}

func (shard *payloadBatcherShard) putRequest(req *writeRequest) {
	req.reset()
	select {
	case shard.reqPool <- req:
	default:
		writeRequestPool.Put(req)
	}
}

func (shard *payloadBatcherShard) startTimer(d time.Duration) <-chan time.Time {
	shard.timer.Reset(d)
	return shard.timer.C
}

func (shard *payloadBatcherShard) stopTimer() {
	if shard.timer == nil {
		return
	}
	if !shard.timer.Stop() {
		select {
		case <-shard.timer.C:
		default:
		}
	}
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

func marshalAllocated(m sizedMarshaler) ([]byte, error) {
	buf := make([]byte, m.Size())
	if err := marshalToSizedBuffer(buf, m); err != nil {
		return nil, err
	}
	return buf, nil
}

func pebblePayloadsBatchSize(payloads []*putPayload) int {
	size := pebbleBatchHeader
	for _, payload := range payloads {
		size += pebblePayloadBatchSize(payload)
	}
	return size
}

func pebblePayloadBatchSize(payload *putPayload) int {
	return pebbleBatchEntrySize(promiseKeyLen(payload.promiseHash), payload.ppSize) +
		pebbleBatchEntrySize(shardKeyLen(payload.promiseHash), payload.shardSize) +
		pebbleBatchEntrySize(pruneKeyLen(payload.promiseHash), 0)
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

func (p *putPayload) applyPebble(batch *pebbledb.Batch) error {
	if err := writePebblePromisePayload(batch, p.promiseHash, p.ppSize, p.promiseProto); err != nil {
		return fmt.Errorf("writing payment promise: %w", err)
	}
	if err := writePebbleShardPayload(batch, p.commitment, p.promiseHash, p.shardSize, p.shard); err != nil {
		return fmt.Errorf("writing shard: %w", err)
	}
	if err := writePebblePrunePayload(batch, p.pruneAt, p.commitment, p.promiseHash); err != nil {
		return fmt.Errorf("writing prune index: %w", err)
	}
	return nil
}

func writePebblePromisePayload(batch *pebbledb.Batch, promiseHash []byte, valueSize int, value sizedMarshaler) error {
	op := batch.SetDeferred(promiseKeyLen(promiseHash), valueSize)
	n := copy(op.Key, promiseKeyPrefix)
	hex.Encode(op.Key[n:], promiseHash)
	if err := marshalToSizedBuffer(op.Value, value); err != nil {
		return err
	}
	if err := op.Finish(); err != nil {
		return fmt.Errorf("finishing pebble batch op: %w", err)
	}
	return nil
}

func writePebbleShardPayload(batch *pebbledb.Batch, commitment Commitment, promiseHash []byte, valueSize int, value sizedMarshaler) error {
	op := batch.SetDeferred(shardKeyLen(promiseHash), valueSize)
	n := copy(op.Key, shardKeyPrefix)
	hex.Encode(op.Key[n:n+commitmentHexLen], commitment[:])
	n += commitmentHexLen
	op.Key[n] = '/'
	n++
	hex.Encode(op.Key[n:], promiseHash)
	if err := marshalToSizedBuffer(op.Value, value); err != nil {
		return err
	}
	if err := op.Finish(); err != nil {
		return fmt.Errorf("finishing pebble batch op: %w", err)
	}
	return nil
}

func writePebblePrunePayload(batch *pebbledb.Batch, pruneAt time.Time, commitment Commitment, promiseHash []byte) error {
	op := batch.SetDeferred(pruneKeyLen(promiseHash), 0)
	n := copy(op.Key, pruneKeyPrefix)
	key := pruneAt.UTC().AppendFormat(op.Key[:n], timestampLayout)
	n = len(key)
	op.Key[n] = '/'
	n++
	hex.Encode(op.Key[n:n+commitmentHexLen], commitment[:])
	n += commitmentHexLen
	op.Key[n] = '/'
	n++
	hex.Encode(op.Key[n:], promiseHash)
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
