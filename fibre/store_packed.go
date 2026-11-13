package fibre

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
)

const (
	// packMetaPrefix keys the per-object member set used to decide when a packed
	// object holds no more live shards and can be deleted.
	packMetaPrefix = "/pack/"
	// packsSubdir is the key segment under which packed objects are stored,
	// parallel to shardsSubdir for one-object-per-shard writes.
	packsSubdir = "packs"
	// maxPackedObjectBytes caps a single packed object; a batch flushes before
	// it would exceed this.
	maxPackedObjectBytes int64 = 4 << 30
	// packedFlushDelay bounds the extra latency packing adds: a partial batch
	// that never fills uploads after this long. It is an internal constant, not
	// a knob, so packing stays a single setting.
	packedFlushDelay = 250 * time.Millisecond
)

var errPackedClosed = errors.New("packed storage closed")

// packedBackend batches several shards into one S3 PUT to stay under object
// request-rate limits. A batch flushes when it reaches shardsPerObject, when it
// would exceed the per-object byte cap, after packedFlushDelay, or on shutdown,
// so nothing waits for a full batch. The byte range of each shard lives in its
// own shard marker (tag 0x06); a small per-object member set tracks when the
// last shard expires so the object can be deleted.
//
// It also serves reads, deletes, and pruning for shards packed earlier even
// when packing is off, so turning it off (shards_per_object back to 1) never
// loses previously-packed shards.
type packedBackend struct {
	object *objectBackend
	db     *pebbledb.DB
	// shardsPerObject above 1 routes new writes through packing. Otherwise the
	// backend only reads, deletes, and prunes shards packed earlier.
	shardsPerObject int
	flushDelay      time.Duration

	mu     sync.Mutex // guards the pending batch below
	queue  []*packedRequest
	size   int64
	timer  *time.Timer
	gen    uint64 // identifies the current batch so a stale timer never fires
	closed bool
	wg     sync.WaitGroup

	// deleteMu serializes member-set updates during pruning. It is separate
	// from mu so a slow object DELETE never blocks uploads.
	deleteMu sync.Mutex
}

// packedRequest is one shard awaiting packing. The caller blocks on done and
// keeps the shard buffers alive until the upload has stopped reading them.
type packedRequest struct {
	commitment  Commitment
	promiseHash []byte
	ppData      []byte // marshaled payment promise proto
	pruneAt     time.Time
	reader      *shardReader
	offset      int64 // byte offset inside the packed object, set at flush
	done        chan struct{}
	err         error
	dispatched  bool
}

func newPackedBackend(object *objectBackend, db *pebbledb.DB, shardsPerObject int) *packedBackend {
	return &packedBackend{object: object, db: db, shardsPerObject: shardsPerObject, flushDelay: packedFlushDelay}
}

func packMetaKey(packID [packIDSize]byte) []byte {
	return []byte(packMetaPrefix + hex.EncodeToString(packID[:]))
}

// packObjectKey derives the S3 key of a packed object from its identity.
func (b *packedBackend) packObjectKey(packID [packIDSize]byte) string {
	return b.object.key(packsSubdir, hex.EncodeToString(packID[:]))
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
	ppData, err := marshalPromise(promise)
	if err != nil {
		return err
	}
	key := shardKey(promise.Commitment, promiseHash)
	req := &packedRequest{
		commitment:  promise.Commitment,
		promiseHash: promiseHash,
		ppData:      ppData,
		pruneAt:     pruneAt,
		reader:      reader,
		done:        make(chan struct{}),
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errPackedClosed
	}
	// Keep Put idempotent for a shard that is already stored, as the plain
	// path is. Concurrent uploads of one shard never get here: the server
	// serializes them per promise hash and re-checks the store first.
	if _, closer, err := b.db.Get(key); err == nil {
		_ = closer.Close()
		b.mu.Unlock()
		return nil // already durably stored
	} else if !errors.Is(err, pebbledb.ErrNotFound) {
		b.mu.Unlock()
		return err
	}
	// Flush the pending batch first if this shard cannot fit the object cap.
	if len(b.queue) > 0 && b.size+reader.size > maxPackedObjectBytes {
		b.flushLocked()
	}
	b.queue = append(b.queue, req)
	b.size += reader.size
	if len(b.queue) == 1 {
		b.armTimerLocked()
	}
	if len(b.queue) >= b.shardsPerObject {
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
		b.size -= req.reader.size
		if len(b.queue) == 0 && b.timer != nil {
			b.timer.Stop()
			b.timer = nil
		}
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
		for _, r := range requests {
			r.err = err
			close(r.done)
		}
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
		body.size += r.reader.size
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
	members := make([][]byte, len(requests))
	for i, r := range requests {
		marker := encodePackedShardMarker(r.reader.size, r.offset, packID)
		if err := setShardMeta(batch, r.commitment, r.promiseHash, r.ppData, marker, r.pruneAt); err != nil {
			return err
		}
		members[i] = r.promiseHash
	}
	if err := batch.Set(packMetaKey(packID), encodePackMembers(members), pebbledb.NoSync); err != nil {
		return err
	}
	return batch.Commit(pebbledb.Sync)
}

// get reads the shard that marker locates with a ranged GET of its object.
func (b *packedBackend) get(ctx context.Context, marker []byte) (*types.BlobShard, error) {
	packID, offset, size, err := decodePackedShardMarker(marker)
	if err != nil {
		return nil, err
	}
	rng := fmt.Sprintf("bytes=%d-%d", offset, offset+size-1)
	return b.object.getShard(ctx, b.packObjectKey(packID), &rng)
}

// delete removes the shard from its object's member set and deletes the object
// once its last shard is gone. It is idempotent: removing a member already
// absent is a no-op, so a retry after a crash never double-frees or drops an
// object that still has live shards. It leaves the shard marker in place for
// the caller (pruning) to delete.
func (b *packedBackend) delete(ctx context.Context, marker, promiseHash []byte) error {
	packID, _, _, err := decodePackedShardMarker(marker)
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
	if len(members) > 0 {
		return b.db.Set(packMetaKey(packID), encodePackMembers(members), pebbledb.Sync)
	}

	dctx, cancel := context.WithTimeout(ctx, b.object.requestTimeout)
	defer cancel()
	_, err = b.object.client.DeleteObject(dctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.object.namespace.Bucket),
		Key:    aws.String(b.packObjectKey(packID)),
	})
	if err != nil && !isObjectNotFound(err) {
		return fmt.Errorf("deleting packed object: %w", err)
	}
	return b.db.Delete(packMetaKey(packID), pebbledb.Sync)
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
