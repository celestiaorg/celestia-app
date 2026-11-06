package fibre

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
)

// packMetaPrefix keys the per-object member set used to decide when a packed
// object holds no more live shards and can be deleted.
const packMetaPrefix = "/pack/"

var errPackedClosed = errors.New("packed storage closed")

// packedBackend batches several shards into one S3 PUT to stay under object
// request-rate limits. A batch flushes when it reaches shardsPerObject, when it
// would exceed the per-object byte cap, after packedFlushDelay, or on shutdown,
// so nothing waits for a full batch. The byte range of each shard lives in its
// own shard marker (tag 0x06); a small per-object member set tracks when the
// last shard expires so the object can be deleted.
//
// It also serves reads, deletes, and pruning for shards packed earlier even
// when enabled is false, so turning packing off (shards_per_object back to 1)
// never loses previously-packed shards.
type packedBackend struct {
	object      *objectBackend
	db          *pebbledb.DB
	shardsPerPK int
	flushDelay  time.Duration
	// enabled routes new writes through packing. When false the backend only
	// reads, deletes, and prunes shards packed before packing was turned off.
	enabled bool

	mu    sync.Mutex // guards the pending batch and inflight set below
	queue []*packedRequest
	size  int64
	timer *time.Timer
	gen   uint64 // identifies the current batch so a stale timer never fires
	// inflight holds every shard whose batch has not yet committed, keyed by
	// shard key, so concurrent duplicate uploads of one shard join the same
	// request instead of landing in two different packs.
	inflight map[string]*packedRequest
	closed   bool
	wg       sync.WaitGroup

	// deleteMu serializes member-set updates during pruning. It is separate
	// from mu so a slow object DELETE never blocks uploads.
	deleteMu sync.Mutex
}

// packedRequest is one shard awaiting packing. The caller blocks on done and
// keeps the shard buffers alive until the upload has stopped reading them.
type packedRequest struct {
	id          string // shard key, the inflight dedup key
	commitment  Commitment
	promiseHash []byte
	ppData      []byte // marshaled payment promise proto
	pruneAt     time.Time
	reader      *shardReader
	size        int64
	offset      int64 // byte offset inside the packed object, set at flush
	done        chan struct{}
	err         error
	dispatched  bool
}

func newPackedBackend(object *objectBackend, db *pebbledb.DB, shardsPerObject int, flushDelay time.Duration) *packedBackend {
	if shardsPerObject < 1 {
		shardsPerObject = 1
	}
	if flushDelay <= 0 {
		flushDelay = packedFlushDelay
	}
	return &packedBackend{object: object, db: db, shardsPerPK: shardsPerObject, flushDelay: flushDelay, inflight: map[string]*packedRequest{}}
}

func (*packedBackend) backendTag() shardBackendTag { return packedObjectBackendTag }

func packMetaKey(packID [packIDSize]byte) []byte {
	return []byte(packMetaPrefix + hex.EncodeToString(packID[:]))
}

// packObjectKey derives the S3 key of a packed object from its identity.
func (b *packedBackend) packObjectKey(packID [packIDSize]byte) string {
	n := b.object.namespace
	return path.Join(n.Prefix, n.ChainID, n.ValidatorAddress, packsSubdir, hex.EncodeToString(packID[:]))
}

// enqueue adds a shard to the pending batch and blocks until its batch is
// uploaded and committed, or the context ends. It returns only once the shard
// is durably stored or the attempt has failed, so the caller's occupancy
// reservation always matches what is stored.
func (b *packedBackend) enqueue(ctx context.Context, promise *PaymentPromise, promiseHash []byte, shard *types.BlobShard, pruneAt time.Time) error {
	reader, err := newShardReader(shard)
	if err != nil {
		return fmt.Errorf("encoding shard object: %w", err)
	}
	if reader.size > maxPackedObjectBytes {
		return fmt.Errorf("shard size %d exceeds packed object limit %d", reader.size, maxPackedObjectBytes)
	}
	ppData, err := marshalPromise(promise)
	if err != nil {
		return err
	}
	key := shardKey(promise.Commitment, promiseHash)
	req := &packedRequest{
		id:          string(key),
		commitment:  promise.Commitment,
		promiseHash: slices.Clone(promiseHash),
		ppData:      ppData,
		pruneAt:     pruneAt,
		reader:      reader,
		size:        reader.size,
		done:        make(chan struct{}),
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errPackedClosed
	}
	// Dedup this shard so it is never packed into two objects (which would
	// strand a member in one and leak it). The committed check and the
	// in-flight check are both under the lock, and a flush removes a request
	// from inflight only after its marker commits, so there is no window where
	// a duplicate sees the shard as neither committed nor in flight.
	if _, closer, err := b.db.Get(key); err == nil {
		_ = closer.Close()
		b.mu.Unlock()
		return nil // already durably stored
	} else if !errors.Is(err, pebbledb.ErrNotFound) {
		b.mu.Unlock()
		return err
	}
	if old := b.inflight[req.id]; old != nil {
		b.mu.Unlock()
		select {
		case <-old.done:
			return old.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	b.inflight[req.id] = req
	// Flush the pending batch first if this shard cannot fit the object cap.
	if len(b.queue) > 0 && b.size+req.size > maxPackedObjectBytes {
		b.flushLocked()
	}
	b.queue = append(b.queue, req)
	b.size += req.size
	if len(b.queue) == 1 {
		b.armTimerLocked()
	}
	if len(b.queue) >= b.shardsPerPK || b.size >= maxPackedObjectBytes {
		b.flushLocked()
	}
	b.mu.Unlock()

	select {
	case <-req.done:
		return req.err
	case <-ctx.Done():
		b.mu.Lock()
		if !req.dispatched {
			b.removeQueuedLocked(req, ctx.Err())
			b.mu.Unlock()
			return ctx.Err()
		}
		b.mu.Unlock()
		// A dispatched batch still reads the caller's buffers; wait for its PUT.
		<-req.done
		return req.err
	}
}

// removeQueuedLocked drops a still-queued request, e.g. after its caller's
// context ended, so it is never uploaded or accounted.
func (b *packedBackend) removeQueuedLocked(req *packedRequest, err error) {
	for i, pending := range b.queue {
		if pending != req {
			continue
		}
		// slices.Delete zeroes the vacated tail slot so the request and its
		// caller buffers can be garbage collected.
		b.queue = slices.Delete(b.queue, i, i+1)
		b.size -= req.size
		if len(b.queue) == 0 && b.timer != nil {
			b.timer.Stop()
			b.timer = nil
		}
		delete(b.inflight, req.id)
		req.err = err
		close(req.done)
		return
	}
}

// armTimerLocked schedules a flush of the current batch so a batch that never
// fills still uploads within flushDelay.
func (b *packedBackend) armTimerLocked() {
	b.gen++
	gen := b.gen
	b.timer = time.AfterFunc(b.flushDelay, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.gen == gen && len(b.queue) > 0 {
			b.flushLocked()
		}
	})
}

// flushLocked detaches the pending batch and uploads it off the lock.
func (b *packedBackend) flushLocked() {
	if len(b.queue) == 0 {
		return
	}
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	requests := b.queue
	b.queue = nil
	b.size = 0
	for _, r := range requests {
		r.dispatched = true
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		err := b.upload(requests)
		// Remove from inflight only after the marker commit inside upload, so a
		// concurrent duplicate sees the shard as committed, not absent.
		b.mu.Lock()
		for _, r := range requests {
			r.err = err
			delete(b.inflight, r.id)
			close(r.done)
		}
		b.mu.Unlock()
	}()
}

// upload writes the batch as one S3 object, then commits every member's
// promise, shard marker, and prune index together with the object's member set
// in a single durable Pebble batch. The commit follows the PUT, so a crash
// between them can leave the object unreferenced (never read, a bounded storage
// leak) but never acknowledges a shard that is not durably recorded.
func (b *packedBackend) upload(requests []*packedRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), b.object.requestTimeout)
	defer cancel()

	var packID [packIDSize]byte
	if _, err := rand.Read(packID[:]); err != nil {
		return err
	}
	body := &shardReader{}
	for _, r := range requests {
		r.offset = body.size
		body.parts = append(body.parts, r.reader.parts...)
		body.size += r.size
	}

	if m := b.object.metrics; m != nil {
		m.packedShards.Record(ctx, int64(len(requests)))
	}
	_, err := b.object.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(b.object.namespace.Bucket),
		Key:           aws.String(b.packObjectKey(packID)),
		Body:          body,
		ContentLength: aws.Int64(body.size),
	}, objectPutOptions)
	if err != nil {
		return fmt.Errorf("putting packed object: %w", err)
	}

	batch := b.db.NewBatch()
	defer batch.Close()
	members := make([]byte, 0, 4+len(requests)*packedMemberSize)
	members = binary.BigEndian.AppendUint32(members, uint32(len(requests)))
	for _, r := range requests {
		if err := batch.Set(promiseKey(r.promiseHash), r.ppData, pebbledb.NoSync); err != nil {
			return err
		}
		marker := encodePackedShardMarker(r.size, r.offset, packID)
		if err := batch.Set(shardKey(r.commitment, r.promiseHash), marker, pebbledb.NoSync); err != nil {
			return err
		}
		if err := batch.Set(pruneKey(r.pruneAt, r.commitment, r.promiseHash), nil, pebbledb.NoSync); err != nil {
			return err
		}
		members = append(members, r.promiseHash...)
	}
	if err := batch.Set(packMetaKey(packID), members, pebbledb.NoSync); err != nil {
		return err
	}
	return batch.Commit(pebbledb.Sync)
}

func (b *packedBackend) Put(context.Context, Commitment, []byte, *types.BlobShard) error {
	// Packed writes go through enqueue, not the routed Put path.
	return errors.New("packed backend does not store single objects")
}

func (b *packedBackend) Get(ctx context.Context, commitment Commitment, promiseHash []byte) (_ *types.BlobShard, err error) {
	done := b.object.metrics.observeBackendGet(ctx, storageBackendObject)
	defer func() { done(err) }()

	packID, offset, size, err := b.markerLocation(commitment, promiseHash)
	if errors.Is(err, pebbledb.ErrNotFound) {
		return nil, ErrStoreNotFound
	}
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, b.object.requestTimeout)
	defer cancel()
	out, err := b.object.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.object.namespace.Bucket),
		Key:    aws.String(b.packObjectKey(packID)),
		Range:  aws.String(fmt.Sprintf("bytes=%d-%d", offset, offset+size-1)),
	})
	if isObjectNotFound(err) {
		return nil, ErrStoreNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting packed object: %w", err)
	}
	defer out.Body.Close()

	limited := &io.LimitedReader{R: b.object.metrics.backendReader(ctx, storageBackendObject, out.Body), N: size + 1}
	reader := bufio.NewReaderSize(limited, 1<<20)
	shard, err := readShardBinary(reader)
	if err != nil {
		return nil, fmt.Errorf("decoding packed shard: %w", err)
	}
	if _, err := reader.ReadByte(); err != io.EOF || limited.N != 1 {
		return nil, fmt.Errorf("%w: packed range has unexpected trailing bytes", ErrStoreIntegrity)
	}
	return shard, nil
}

// Has reports presence from the committed shard marker alone: a packed marker
// is committed only after its object's PUT succeeds, so the object exists
// whenever the marker does. No HEAD request is needed.
func (b *packedBackend) Has(_ context.Context, commitment Commitment, promiseHash []byte) (bool, error) {
	_, _, _, err := b.markerLocation(commitment, promiseHash)
	if errors.Is(err, pebbledb.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Delete removes the shard from its object's member set and deletes the object
// once its last shard is gone. It is idempotent: removing a member already
// absent is a no-op, so a retry after a crash never double-frees or drops an
// object that still has live shards. It leaves the shard marker in place for
// the caller (pruning) to delete.
func (b *packedBackend) Delete(ctx context.Context, commitment Commitment, promiseHash []byte) error {
	packID, _, _, err := b.markerLocation(commitment, promiseHash)
	if errors.Is(err, pebbledb.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	b.deleteMu.Lock()
	defer b.deleteMu.Unlock()
	metaData, closer, err := b.db.Get(packMetaKey(packID))
	if errors.Is(err, pebbledb.ErrNotFound) {
		return nil // the object was already fully deleted
	}
	if err != nil {
		return err
	}
	// Pebble owns metaData until the closer closes; clone it so the member
	// slices stay valid while we rewrite them.
	metaData = slices.Clone(metaData)
	closer.Close()
	members, err := decodePackMembers(metaData)
	if err != nil {
		return err
	}
	members = slices.DeleteFunc(members, func(h []byte) bool { return string(h) == string(promiseHash) })

	batch := b.db.NewBatch()
	defer batch.Close()
	if len(members) == 0 {
		dctx, cancel := context.WithTimeout(ctx, b.object.requestTimeout)
		defer cancel()
		_, err = b.object.client.DeleteObject(dctx, &s3.DeleteObjectInput{
			Bucket: aws.String(b.object.namespace.Bucket),
			Key:    aws.String(b.packObjectKey(packID)),
		})
		if err != nil && !isObjectNotFound(err) {
			return fmt.Errorf("deleting packed object: %w", err)
		}
		if err := batch.Delete(packMetaKey(packID), pebbledb.NoSync); err != nil {
			return err
		}
	} else {
		if err := batch.Set(packMetaKey(packID), encodePackMembers(members), pebbledb.NoSync); err != nil {
			return err
		}
	}
	return batch.Commit(pebbledb.Sync)
}

// markerLocation reads the shard's packed marker from Pebble. Reads and deletes
// receive only the commitment and promise hash, so the location is recovered
// from the marker rather than carried in by the caller.
func (b *packedBackend) markerLocation(commitment Commitment, promiseHash []byte) (packID [packIDSize]byte, offset, size int64, err error) {
	data, closer, err := b.db.Get(shardKey(commitment, promiseHash))
	if err != nil {
		return packID, 0, 0, err
	}
	defer closer.Close()
	return decodePackedShardMarker(data)
}

// Close flushes any partial batch, uploading it, and waits for every in-flight
// upload to finish before the Pebble database it indexes is closed.
func (b *packedBackend) Close() {
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		b.flushLocked()
	}
	b.mu.Unlock()
	// Every caller waits: a second Close must not let the database close while
	// the first Close's uploads are still in flight.
	b.wg.Wait()
}

const packedMemberSize = 32 // sha256 promise hash

func encodePackMembers(hashes [][]byte) []byte {
	buf := binary.BigEndian.AppendUint32(make([]byte, 0, 4+len(hashes)*packedMemberSize), uint32(len(hashes)))
	for _, h := range hashes {
		buf = append(buf, h...)
	}
	return buf
}

func decodePackMembers(data []byte) ([][]byte, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("%w: pack member set too short", ErrStoreIntegrity)
	}
	count := int(binary.BigEndian.Uint32(data[:4]))
	if len(data) != 4+count*packedMemberSize {
		return nil, fmt.Errorf("%w: pack member set has %d bytes for %d members", ErrStoreIntegrity, len(data), count)
	}
	hashes := make([][]byte, count)
	for i := range count {
		start := 4 + i*packedMemberSize
		hashes[i] = data[start : start+packedMemberSize]
	}
	return hashes, nil
}

// openPacked attaches a packed backend when packing is enabled, or when
// previously-packed shards still need reads and pruning. It returns nil without
// attaching one when packing was never used, leaving the one-object-per-shard
// path exactly as before.
func (s *routedStorage) openPacked(ctx context.Context, cfg StoreConfig, db *pebbledb.DB, object shardBackend) error {
	hasPacked, err := hasBackendMarkers(ctx, db, packedObjectBackendTag)
	if err != nil {
		return err
	}
	enabled := cfg.StorageBackend == storageBackendObject && cfg.ObjectStorage.ShardsPerObject > 1
	if !enabled && !hasPacked {
		return nil
	}
	backend, ok := object.(*objectBackend)
	if !ok {
		return fmt.Errorf("%w: packed shards require object storage", ErrStoreIntegrity)
	}
	b := newPackedBackend(backend, db, cfg.ObjectStorage.ShardsPerObject, packedFlushDelay)
	b.enabled = enabled
	s.packed = b
	return nil
}
