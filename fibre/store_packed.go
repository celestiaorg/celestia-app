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
	// it would exceed this. Together with maxPackedShardBytes it bounds how long
	// one PUT, and so every shard waiting on it, can take.
	maxPackedObjectBytes int64 = 256 << 20
	// maxPackedShardBytes is the largest shard that is packed. Larger shards
	// gain little from packing and would let one upload slow a whole batch, so
	// they take the one-object-per-shard path.
	maxPackedShardBytes int64 = 4 << 20
	// packedMinThroughput is the slowest upload rate a packed PUT is allowed:
	// its timeout is the per-request timeout plus the time to send the object
	// at this rate, so a full object gets as long as a small one plus 32s.
	packedMinThroughput int64 = 8 << 20
	// packedFlushDelay bounds the extra latency packing adds: a partial batch
	// that never fills uploads after this long. It is an internal constant, not
	// a knob, so packing stays a single setting.
	packedFlushDelay = 250 * time.Millisecond
)

var (
	errPackedClosed = errors.New("packed storage closed")
	// errPackedPut marks a failed packed PUT. Store.Put then stores each member
	// on its own, so one bad batch never fails the other uploads in it.
	errPackedPut = errors.New("putting packed object")
)

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
	// Keep Put idempotent for a shard that is already stored, as the plain path
	// is, but store it again if its packed object has gone missing (e.g. deleted
	// by a bucket lifecycle rule); upload then moves it out of the old object's
	// member set. Concurrent uploads of one shard never get here: the server
	// serializes them per promise hash and re-checks the store first.
	if stored, err := b.stored(ctx, shardKey(promise.Commitment, promiseHash)); err != nil || stored {
		return err
	}
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

// stored reports whether the shard at key is already stored with its payload.
// Only a packed marker whose object is missing counts as not stored; any other
// existing marker is left to its own backend, as before packing.
func (b *packedBackend) stored(ctx context.Context, key []byte) (bool, error) {
	marker, closer, err := b.db.Get(key)
	if errors.Is(err, pebbledb.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	marker = slices.Clone(marker)
	_ = closer.Close()
	if tag, _, err := decodeShardMarkerBackend(marker); err != nil || tag != packedObjectBackendTag {
		return true, nil
	}
	return b.has(ctx, marker)
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
	b.wg.Go(func() {
		err := b.upload(requests)
		for _, r := range requests {
			r.err = err
			close(r.done)
		}
	})
}

// upload writes the batch as one S3 object, then commits every member's
// promise, shard marker, and prune index together with the object's member set
// in a single durable Pebble batch. The commit follows the PUT, so a crash
// between them can leave the object unreferenced (never read, a bounded storage
// leak) but never acknowledges a shard that is not durably recorded.
func (b *packedBackend) upload(requests []*packedRequest) error {
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

	ctx, cancel := context.WithTimeout(context.Background(), packedUploadTimeout(b.object.requestTimeout, body.size))
	defer cancel()
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
		return fmt.Errorf("%w: %w", errPackedPut, err)
	}

	// deleteMu serializes this read-modify-write of older objects' member sets
	// with pruning.
	b.deleteMu.Lock()
	defer b.deleteMu.Unlock()
	batch := b.db.NewBatch()
	defer batch.Close()
	members := make([][]byte, len(requests))
	moved := map[[packIDSize]byte][][]byte{} // older object -> members leaving it
	for i, r := range requests {
		key := shardKey(r.commitment, r.promiseHash)
		if old, closer, err := b.db.Get(key); err == nil {
			if oldID, _, _, err := decodePackedShardMarker(old); err == nil {
				moved[oldID] = append(moved[oldID], r.promiseHash)
			}
			_ = closer.Close()
		} else if !errors.Is(err, pebbledb.ErrNotFound) {
			return err
		}
		marker := encodePackedShardMarker(r.reader.size, r.offset, packID)
		if err := setShardMeta(batch, r.commitment, r.promiseHash, r.ppData, marker, r.pruneAt); err != nil {
			return err
		}
		members[i] = r.promiseHash
	}
	if err := batch.Set(packMetaKey(packID), encodePackMembers(members), pebbledb.NoSync); err != nil {
		return err
	}
	var emptied [][packIDSize]byte
	for oldID, hashes := range moved {
		empty, err := b.removeMembers(batch, oldID, hashes)
		if err != nil {
			return err
		}
		if empty {
			emptied = append(emptied, oldID)
		}
	}
	if err := batch.Commit(pebbledb.Sync); err != nil {
		return err
	}
	// A re-stored shard's old object is normally gone already; delete it in
	// case it is not. A failure here only leaks the object, never a shard.
	for _, oldID := range emptied {
		_ = b.deleteObject(context.Background(), oldID)
	}
	return nil
}

// packedUploadTimeout gives a packed PUT the per-request timeout plus the time
// to send size bytes at packedMinThroughput.
func packedUploadTimeout(requestTimeout time.Duration, size int64) time.Duration {
	return requestTimeout + time.Duration(size)*time.Second/time.Duration(packedMinThroughput)
}

// has reports whether the packed object holding marker's shard still exists.
func (b *packedBackend) has(ctx context.Context, marker []byte) (bool, error) {
	packID, _, _, err := decodePackedShardMarker(marker)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, b.object.requestTimeout)
	defer cancel()
	_, err = b.object.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.object.namespace.Bucket),
		Key:    aws.String(b.packObjectKey(packID)),
	})
	switch {
	case isObjectNotFound(err):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("checking packed object: %w", err)
	default:
		return true, nil
	}
}

// removeMembers drops hashes from packID's member set in batch and reports
// whether the set is now empty, in which case it deletes the set too. The
// caller holds deleteMu.
func (b *packedBackend) removeMembers(batch *pebbledb.Batch, packID [packIDSize]byte, hashes [][]byte) (bool, error) {
	metaData, closer, err := b.db.Get(packMetaKey(packID))
	if errors.Is(err, pebbledb.ErrNotFound) {
		return false, nil // the object was already fully deleted
	}
	if err != nil {
		return false, err
	}
	// Pebble owns metaData until the closer closes; clone it so the member
	// slices stay valid while we rewrite them.
	metaData = slices.Clone(metaData)
	_ = closer.Close()
	members, err := decodePackMembers(metaData)
	if err != nil {
		return false, err
	}
	members = slices.DeleteFunc(members, func(h []byte) bool {
		return slices.ContainsFunc(hashes, func(x []byte) bool { return string(x) == string(h) })
	})
	if len(members) > 0 {
		return false, batch.Set(packMetaKey(packID), encodePackMembers(members), pebbledb.NoSync)
	}
	return true, batch.Delete(packMetaKey(packID), pebbledb.NoSync)
}

func (b *packedBackend) deleteObject(ctx context.Context, packID [packIDSize]byte) error {
	ctx, cancel := context.WithTimeout(ctx, b.object.requestTimeout)
	defer cancel()
	_, err := b.object.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.object.namespace.Bucket),
		Key:    aws.String(b.packObjectKey(packID)),
	})
	if err != nil && !isObjectNotFound(err) {
		return fmt.Errorf("deleting packed object: %w", err)
	}
	return nil
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
	batch := b.db.NewBatch()
	defer batch.Close()
	empty, err := b.removeMembers(batch, packID, [][]byte{promiseHash})
	if err != nil {
		return err
	}
	if empty {
		// Delete the object before its member set, so a failed DELETE is
		// retried at the next prune instead of leaking the object.
		if err := b.deleteObject(ctx, packID); err != nil {
			return err
		}
	}
	return batch.Commit(pebbledb.Sync)
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
