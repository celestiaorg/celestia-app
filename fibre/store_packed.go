package fibre

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/middleware"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/semaphore"
)

const (
	packedNamespaceKey         = "/meta/object-namespace-packed"
	packedManifestPrefix       = "/pack/manifest/"
	maxPackedBytes       int64 = 4 << 30
	packedAdmissionBytes int64 = 128 << 30
)

type packedLocation struct {
	Key            string
	Offset, Length int64
}
type packedManifest struct {
	Key     string
	Ready   bool
	Members map[string]packedLocation
}
type packedQueue struct {
	requests []*packedRequest
}

type packedRequest struct {
	id         string
	hash       []byte
	reader     *shardReader
	done       chan struct{}
	err        error
	dispatched bool
	queueDone  func()
}

// packedBackend keeps shard locations and a recovery journal in Pebble.
type packedBackend struct {
	object    *objectBackend
	db        *pebbledb.DB
	batchSize int
	memory    *semaphore.Weighted
	mu        sync.Mutex
	queues    [8]packedQueue
	inflight  map[string]*packedRequest
	closed    bool
	wg        sync.WaitGroup
}

func newPackedBackend(object *objectBackend, db *pebbledb.DB, batchSize int) *packedBackend {
	return &packedBackend{object: object, db: db, batchSize: batchSize, memory: semaphore.NewWeighted(packedAdmissionBytes), inflight: make(map[string]*packedRequest)}
}
func (*packedBackend) backendTag() shardBackendTag { return packedObjectBackendTag }
func packedIndexKey(id string) []byte              { return []byte("/pack/index/" + id) }
func packedManifestKey(key string) []byte          { return []byte(packedManifestPrefix + key) }
func (b *packedBackend) location(id string) (packedLocation, error) {
	var loc packedLocation
	data, closer, err := b.db.Get(packedIndexKey(id))
	if err != nil {
		return loc, err
	}
	defer closer.Close()
	if err = json.Unmarshal(data, &loc); err != nil {
		return loc, fmt.Errorf("%w: packed location: %v", ErrStoreIntegrity, err)
	}
	if loc.Key == "" || loc.Offset < 0 || loc.Length <= 0 || loc.Offset > maxPackedBytes-loc.Length {
		return loc, fmt.Errorf("%w: invalid packed location", ErrStoreIntegrity)
	}
	return loc, nil
}
func (b *packedBackend) Put(ctx context.Context, c Commitment, h []byte, shard *types.BlobShard) error {
	reader, err := newShardReader(shard)
	if err != nil {
		return err
	}
	if reader.size > maxPackedBytes/int64(b.batchSize) || reader.size > packedAdmissionBytes/int64(8*b.batchSize) {
		return fmt.Errorf("shard size %d cannot form a full batch of %d within object and admission limits", reader.size, b.batchSize)
	}
	admissionDone := b.object.metrics.phase(ctx, "packed_admission_wait", reader.size)
	err = b.memory.Acquire(ctx, reader.size)
	admissionDone()
	if err != nil {
		return err
	}
	defer b.memory.Release(reader.size)
	defer b.object.metrics.phase(ctx, "packed_admitted", reader.size)()
	id := string(shardKey(c, h))
	lockDone := b.object.metrics.phase(ctx, "packed_mutex_wait", 0)
	b.mu.Lock()
	lockDone()
	if b.closed {
		b.mu.Unlock()
		return errors.New("packed storage closed")
	}
	if _, err = b.location(id); err == nil {
		b.mu.Unlock()
		return nil
	} else if !errors.Is(err, pebbledb.ErrNotFound) {
		b.mu.Unlock()
		return err
	}
	if old := b.inflight[id]; old != nil {
		b.mu.Unlock()
		select {
		case <-old.done:
			return old.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	req := &packedRequest{id: id, hash: append([]byte(nil), h...), reader: reader, done: make(chan struct{})}
	req.queueDone = b.object.metrics.phase(ctx, "packed_queue_fill", reader.size)
	b.inflight[id] = req
	group := 0
	if len(h) > 0 {
		group = int(h[0] & 7)
	}
	q := &b.queues[group]
	q.requests = append(q.requests, req)
	if len(q.requests) >= b.batchSize {
		b.flushLocked(group)
	}
	b.mu.Unlock()
	select {
	case <-req.done:
		return req.err
	case <-ctx.Done():
		b.mu.Lock()
		if !req.dispatched {
			for i, pending := range q.requests {
				if pending == req {
					q.requests = slices.Delete(q.requests, i, i+1)
					b.failQueuedLocked(req, ctx.Err())
					break
				}
			}
		}
		b.mu.Unlock()
		// Dispatched batches retain caller buffers until the PUT has stopped reading.
		<-req.done
		return req.err
	}

}
func (b *packedBackend) failQueuedLocked(req *packedRequest, err error) {
	req.queueDone()
	req.err = err
	delete(b.inflight, req.id)
	close(req.done)
}
func (b *packedBackend) flushLocked(group int) {
	q := &b.queues[group]
	if len(q.requests) != b.batchSize {
		return
	}
	requests := q.requests
	q.requests = nil
	for _, r := range requests {
		r.dispatched = true
		r.queueDone()
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		err := b.upload(requests)
		b.mu.Lock()
		defer b.mu.Unlock()
		for _, r := range requests {
			r.err = err
			delete(b.inflight, r.id)
			close(r.done)
		}
	}()
}
func (b *packedBackend) upload(requests []*packedRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), b.object.requestTimeout)
	defer cancel()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	group := byte(0)
	if len(requests[0].hash) > 0 {
		group = requests[0].hash[0] & 7
	}
	key := fmt.Sprintf("%02x/", group) + path.Join(b.object.namespace.Prefix, b.object.namespace.ChainID, b.object.namespace.ValidatorAddress, "packs", hex.EncodeToString(requests[0].hash), hex.EncodeToString(nonce[:]))
	manifest := packedManifest{Key: key, Members: make(map[string]packedLocation, len(requests))}
	reader := &shardReader{}
	for _, r := range requests {
		manifest.Members[r.id] = packedLocation{key, reader.size, r.reader.size}
		reader.parts = append(reader.parts, r.reader.parts...)
		reader.size += r.reader.size
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	intentDone := b.object.metrics.phase(ctx, "packed_intent_sync", 0)
	err = b.db.Set(packedManifestKey(key), data, pebbledb.Sync)
	intentDone()
	if err != nil {
		return err
	}
	if m := b.object.metrics; m != nil {
		m.packedShards.Record(ctx, int64(len(requests)))
	}
	putDone := b.object.metrics.phase(ctx, "packed_put", reader.size)
	_, err = b.object.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(b.object.namespace.Bucket), Key: aws.String(key), Body: reader, ContentLength: aws.Int64(reader.size), IfNoneMatch: aws.String("*")}, func(o *s3.Options) {
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.APIOptions = append(o.APIOptions, v4.SwapComputePayloadSHA256ForUnsignedPayloadMiddleware, func(stack *middleware.Stack) error {
			return stack.Finalize.Add(middleware.FinalizeMiddlewareFunc("PackedPutAttempts", func(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
				out, metadata, err := next.HandleFinalize(ctx, in)
				if m := b.object.metrics; m != nil {
					m.packedPutAttempts.Add(ctx, 1, metric.WithAttributes(attribute.Bool("success", err == nil)))
				}
				return out, metadata, err
			}), middleware.After)
		})
	})
	putDone()
	if err != nil && !hasObjectErrorCode(err, "PreconditionFailed") {
		return fmt.Errorf("putting packed object: %w", err)
	}
	manifest.Ready = true
	batch := b.db.NewBatch()
	defer batch.Close()
	for id, loc := range manifest.Members {
		data, _ = json.Marshal(loc)
		if err = batch.Set(packedIndexKey(id), data, nil); err != nil {
			return err
		}
	}
	data, _ = json.Marshal(manifest)
	if err = batch.Set(packedManifestKey(key), data, nil); err != nil {
		return err
	}
	indexDone := b.object.metrics.phase(ctx, "packed_index_sync", 0)
	err = batch.Commit(pebbledb.Sync)
	indexDone()
	return err
}
func (b *packedBackend) Get(ctx context.Context, c Commitment, h []byte) (_ *types.BlobShard, err error) {
	done := b.object.metrics.observeBackendGet(ctx, storageBackendObject)
	defer func() { done(err) }()
	loc, err := b.location(string(shardKey(c, h)))
	if errors.Is(err, pebbledb.ErrNotFound) {
		return nil, ErrStoreNotFound
	}
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, b.object.requestTimeout)
	defer cancel()
	out, err := b.object.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(b.object.namespace.Bucket), Key: aws.String(loc.Key), Range: aws.String(fmt.Sprintf("bytes=%d-%d", loc.Offset, loc.Offset+loc.Length-1))})
	if isObjectNotFound(err) {
		return nil, ErrStoreNotFound
	}
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	limited := &io.LimitedReader{R: b.object.metrics.backendReader(ctx, storageBackendObject, out.Body), N: loc.Length + 1}
	reader := bufio.NewReaderSize(limited, 1<<20)
	shard, err := readShardBinary(reader)
	if err != nil {
		return nil, err
	}
	if _, err = reader.ReadByte(); err != io.EOF || limited.N != 1 {
		return nil, fmt.Errorf("%w: packed range has trailing bytes", ErrStoreIntegrity)
	}
	return shard, nil
}
func (b *packedBackend) Has(ctx context.Context, c Commitment, h []byte) (bool, error) {
	loc, err := b.location(string(shardKey(c, h)))
	if errors.Is(err, pebbledb.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, b.object.requestTimeout)
	defer cancel()
	_, err = b.object.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(b.object.namespace.Bucket), Key: aws.String(loc.Key)})
	if isObjectNotFound(err) {
		return false, nil
	}
	return err == nil, err
}
func (b *packedBackend) Delete(ctx context.Context, c Commitment, h []byte) error {
	waitDone := b.object.metrics.phase(ctx, "packed_delete_mutex_wait", 0)
	b.mu.Lock()
	waitDone()
	defer b.object.metrics.phase(ctx, "packed_delete_mutex_hold", 0)()
	defer b.mu.Unlock()
	return b.deleteLocked(ctx, string(shardKey(c, h)))
}
func (b *packedBackend) deleteLocked(ctx context.Context, id string) error {
	loc, err := b.location(id)
	if errors.Is(err, pebbledb.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	data, closer, err := b.db.Get(packedManifestKey(loc.Key))
	if err != nil {
		return err
	}
	var manifest packedManifest
	err = json.Unmarshal(data, &manifest)
	closer.Close()
	if err != nil {
		return err
	}
	delete(manifest.Members, id)
	batch := b.db.NewBatch()
	defer batch.Close()
	if len(manifest.Members) == 0 {
		ctx, cancel := context.WithTimeout(ctx, b.object.requestTimeout)
		defer cancel()
		_, err = b.object.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(b.object.namespace.Bucket), Key: aws.String(loc.Key)})
		if err != nil && !isObjectNotFound(err) {
			return err
		}
		err = batch.Delete(packedManifestKey(loc.Key), nil)
	} else {
		data, _ = json.Marshal(manifest)
		err = batch.Set(packedManifestKey(loc.Key), data, nil)
	}
	if err != nil {
		return err
	}
	if err = batch.Delete(packedIndexKey(id), nil); err != nil {
		return err
	}
	return batch.Commit(pebbledb.Sync)
}

// recover removes uploads that never acquired durable shard markers.
func (b *packedBackend) recover(ctx context.Context) error {
	iter, err := b.db.NewIter(&pebbledb.IterOptions{LowerBound: []byte(packedManifestPrefix), UpperBound: prefixUpperBound([]byte(packedManifestPrefix))})
	if err != nil {
		return err
	}
	defer iter.Close()
	for valid := iter.First(); valid; valid = iter.Next() {
		var manifest packedManifest
		if err = json.Unmarshal(iter.Value(), &manifest); err != nil {
			return err
		}
		if !manifest.Ready {
			_, err = b.object.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(b.object.namespace.Bucket), Key: aws.String(manifest.Key)})
			if err != nil && !isObjectNotFound(err) {
				return err
			}
			if err = b.db.Delete(packedManifestKey(manifest.Key), pebbledb.Sync); err != nil {
				return err
			}
			continue
		}
		for id := range manifest.Members {
			_, closer, getErr := b.db.Get([]byte(id))
			if getErr == nil {
				closer.Close()
				continue
			}
			if !errors.Is(getErr, pebbledb.ErrNotFound) {
				return getErr
			}
			if err = b.deleteLocked(ctx, id); err != nil {
				return err
			}
		}
	}
	return iter.Error()
}
func (b *packedBackend) Close() {
	b.mu.Lock()
	b.closed = true
	for group := range b.queues {
		for _, r := range b.queues[group].requests {
			b.failQueuedLocked(r, errors.New("packed storage closed before full batch"))
		}
		b.queues[group].requests = nil
	}
	b.mu.Unlock()
	b.wg.Wait()
}

func (s *routedStorage) openPacked(ctx context.Context, cfg StoreConfig, db *pebbledb.DB) error {
	saved, recorded, err := readObjectNamespaceAt(db, packedNamespaceKey)
	if err != nil {
		return err
	}
	has, err := hasBackendMarkers(ctx, db, packedObjectBackendTag)
	if err != nil {
		return err
	}
	target := cfg.ObjectStorage
	if target.BatchSize == 0 {
		target.BatchSize = 16
	}
	enabled := cfg.StorageBackend == storageBackendObject && target.BatchSize > 1
	if !enabled && !recorded && !has {
		return nil
	}
	if has && !recorded {
		return fmt.Errorf("%w: packed objects lack namespace", ErrStoreIntegrity)
	}
	namespace := saved
	if enabled {
		primary, ok := s.primary.(*objectBackend)
		if !ok {
			return fmt.Errorf("packed storage requires object primary")
		}
		namespace = primary.namespace
		if target.PackedBucket != "" {
			namespace.Bucket = target.PackedBucket
		}
		iter, iterErr := db.NewIter(&pebbledb.IterOptions{LowerBound: []byte(packedManifestPrefix), UpperBound: prefixUpperBound([]byte(packedManifestPrefix))})
		if iterErr != nil {
			return iterErr
		}
		journals := iter.First()
		iterErr = iter.Error()
		iter.Close()
		if iterErr != nil {
			return iterErr
		}
		if recorded && (has || journals) && namespace != saved {
			return fmt.Errorf("%w: packed object namespace changed", ErrStoreIntegrity)
		}
	}
	target.objectNamespace = namespace
	if err = target.Validate(); err != nil {
		return err
	}
	client, err := newObjectClient(ctx, target)
	if err != nil {
		return err
	}
	if !recorded || namespace != saved {
		if err = saveObjectNamespaceAt(db, packedNamespaceKey, namespace); err != nil {
			return err
		}
	}
	object := newObjectBackend(client, namespace)
	object.requestTimeout = target.RequestTimeout
	b := newPackedBackend(object, db, target.BatchSize)
	if !enabled {
		b.batchSize = 1
	}
	if err = b.recover(ctx); err != nil {
		return fmt.Errorf("recovering packed storage: %w", err)
	}
	s.packed = b
	return nil
}
