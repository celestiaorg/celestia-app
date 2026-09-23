package fibre

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/vfs"
)

// shardBackend stores shard payloads in one storage backend.
type shardBackend interface {
	backendTag() shardBackendTag
	Put(context.Context, Commitment, []byte, *types.BlobShard) error
	Get(context.Context, Commitment, []byte) (*types.BlobShard, error)
	Has(context.Context, Commitment, []byte) (bool, error)
	Delete(context.Context, Commitment, []byte) error
}

// routedStorage writes to its primary backend and routes existing markers by tag.
type routedStorage struct {
	metrics    *serverMetrics
	primary    shardBackend
	secondary  shardBackend
	tertiary   shardBackend
	quaternary shardBackend
	quinary    shardBackend
	packed     *packedBackend
}

func newRoutedStorage(primary, secondary shardBackend) *routedStorage {
	return &routedStorage{primary: primary, secondary: secondary}
}

func openRoutedStorage(ctx context.Context, cfg StoreConfig, db *pebbledb.DB, filesystem vfs.FS) (result *routedStorage, resultErr error) {
	defer func() {
		if resultErr == nil {
			resultErr = result.openPacked(ctx, cfg, db)
		}
	}()
	local, err := newLocalBackend(cfg.Path, filesystem)
	if err != nil {
		return nil, fmt.Errorf("opening local shard storage: %w", err)
	}
	object, err := openObjectBackend(ctx, cfg, db)
	if err != nil {
		return nil, fmt.Errorf("opening object shard storage: %w", err)
	}
	hashed, err := openHashedObjectBackend(ctx, cfg, db)
	if err != nil {
		return nil, fmt.Errorf("opening hash-first storage: %w", err)
	}
	next, err := openHashedObjectGeneration(ctx, cfg, db, cfg.ObjectStorage.HashFirstBucketNext, nextHashedObjectNamespaceKey, nextHashedObjectBackendTag)
	if err != nil {
		return nil, fmt.Errorf("opening next hash-first storage: %w", err)
	}
	promiseBucket := ""
	if cfg.ObjectStorage.PromiseHashKeys {
		promiseBucket = cfg.ObjectStorage.HashFirstBucketNext
		if promiseBucket == "" {
			return nil, fmt.Errorf("promise_hash_keys requires hash_first_bucket_next")
		}
	}
	promise, err := openHashedObjectGeneration(ctx, cfg, db, promiseBucket, promiseHashObjectNamespaceKey, promiseHashObjectBackendTag)
	if err != nil {
		return nil, fmt.Errorf("opening promise-hash storage: %w", err)
	}
	if cfg.StorageBackend == storageBackendObject {
		if promise != nil {
			storage := &routedStorage{primary: promise, secondary: object, tertiary: local}
			if hashed != nil {
				storage.quaternary = hashed
			}
			if next != nil {
				storage.quinary = next
			}
			return storage, nil
		}
		if next != nil {
			storage := &routedStorage{primary: next, secondary: object, tertiary: local}
			if hashed != nil {
				storage.quaternary = hashed
			}
			return storage, nil
		}
		if hashed != nil {
			return &routedStorage{primary: hashed, secondary: object, tertiary: local}, nil
		}
		return newRoutedStorage(object, local), nil
	}
	storage := newRoutedStorage(local, object)
	if hashed != nil {
		storage.tertiary = hashed
	}
	if next != nil {
		storage.quaternary = next
	}
	if promise != nil {
		storage.quinary = promise
	}
	return storage, nil
}

func (s *routedStorage) setMetrics(metrics *serverMetrics) {
	s.metrics = metrics
	if s.packed != nil {
		s.packed.object.metrics = metrics
	}
	for _, item := range []shardBackend{s.primary, s.secondary, s.tertiary, s.quaternary, s.quinary} {
		switch backend := item.(type) {
		case *localBackend:
			backend.metrics = metrics
		case *objectBackend:
			backend.metrics = metrics
		}
	}
}

func (s *routedStorage) marker(size int64) []byte {
	if s.packed != nil && s.packed.batchSize > 1 && s.primary.backendTag() != localBackendTag {
		return encodeShardMarkerForBackend(packedObjectBackendTag, size)
	}
	return encodeShardMarkerForBackend(s.primary.backendTag(), size)
}

func (s *routedStorage) Put(ctx context.Context, marker []byte, commitment Commitment, promiseHash []byte, shard *types.BlobShard) error {
	backend, err := s.backendForMarker(marker)
	if err != nil {
		return err
	}
	return backend.Put(ctx, commitment, promiseHash, shard)
}

func (s *routedStorage) Get(ctx context.Context, marker []byte, commitment Commitment, promiseHash []byte) (*types.BlobShard, error) {
	backend, err := s.backendForMarker(marker)
	if err != nil {
		return nil, err
	}
	return backend.Get(ctx, commitment, promiseHash)
}

func (s *routedStorage) Has(ctx context.Context, marker []byte, commitment Commitment, promiseHash []byte) (bool, error) {
	backend, err := s.backendForMarker(marker)
	if err != nil {
		return false, err
	}
	return backend.Has(ctx, commitment, promiseHash)
}

func (s *routedStorage) Delete(ctx context.Context, marker []byte, commitment Commitment, promiseHash []byte) error {
	backend, err := s.backendForMarker(marker)
	if err != nil {
		return err
	}
	return backend.Delete(ctx, commitment, promiseHash)
}

type markedShard struct {
	id     shardID
	marker []byte
}

// DeleteBatch deletes payloads by marker and returns their successful input positions.
// The caller keeps metadata for failed deletions and retries them in a later prune pass.
// Object requests contain at most 1,000 keys.
func (s *routedStorage) DeleteBatch(ctx context.Context, shards []markedShard) ([]int, error) {
	var local []int
	objects := make(map[*objectBackend][]int)
	for i, shard := range shards {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		backend, err := s.backendForMarker(shard.marker)
		if err != nil {
			return nil, err
		}
		if backend.backendTag() == objectBackendTag || backend.backendTag() == hashedObjectBackendTag || backend.backendTag() == nextHashedObjectBackendTag || backend.backendTag() == promiseHashObjectBackendTag {
			object, ok := backend.(*objectBackend)
			if !ok {
				return nil, fmt.Errorf("%w: object backend does not support batch deletion", ErrStoreIntegrity)
			}
			objects[object] = append(objects[object], i)
		} else {
			local = append(local, i)
		}
	}

	var successful []int
	var deleteErr error
	for _, i := range local {
		if err := ctx.Err(); err != nil {
			return successful, errors.Join(deleteErr, err)
		}
		shard := shards[i]
		if err := s.Delete(ctx, shard.marker, shard.id.commitment, shard.id.promiseHash); err != nil {
			if ctx.Err() != nil {
				return successful, errors.Join(deleteErr, err, ctx.Err())
			}
			if deleteErr == nil {
				deleteErr = err
			}
			continue
		}
		successful = append(successful, i)
	}
	for object, group := range objects {
		for indices := range slices.Chunk(group, maxObjectDeleteBatchSize) {
			ids := make([]shardID, len(indices))
			for i, index := range indices {
				ids[i] = shards[index].id
			}
			results, err := object.DeleteObjects(ctx, ids)
			if err != nil {
				if ctx.Err() != nil {
					return successful, errors.Join(deleteErr, err, ctx.Err())
				}
				deleteErr = errors.Join(deleteErr, err)
				continue
			}
			for i, err := range results {
				if err != nil {
					if deleteErr == nil {
						deleteErr = err
					}
					continue
				}
				successful = append(successful, indices[i])
			}
		}
	}
	if deleteErr != nil {
		return successful, &partialDeleteError{deleteErr}
	}
	return successful, nil
}

// partialDeleteError reports payload failures after all deletions were attempted.
type partialDeleteError struct{ err error }

func (e *partialDeleteError) Error() string { return e.err.Error() }
func (e *partialDeleteError) Unwrap() error { return e.err }

func (s *routedStorage) size(marker []byte, commitment Commitment, promiseHash []byte) (int64, error) {
	tag, size, err := decodeShardMarkerBackend(marker)
	if err != nil {
		return 0, err
	}
	if _, err := s.backend(tag); err != nil {
		return 0, err
	}
	if size > 0 {
		return size, nil
	}
	local, err := s.localBackend()
	if err != nil {
		return 0, err
	}
	return local.size(commitment, promiseHash)
}

func (s *routedStorage) diskAvailable() (int64, error) {
	local, err := s.localBackend()
	if err != nil {
		return 0, err
	}
	return local.diskAvailable()
}

// resetStaging removes incomplete local writes, including writes left before a switch to object mode.
// Object backends do not use the local staging directory.
func (s *routedStorage) resetStaging() (int, error) {
	local, err := s.localBackend()
	if err != nil {
		return 0, err
	}
	return local.resetStaging()
}

func (s *routedStorage) backendForMarker(marker []byte) (shardBackend, error) {
	tag, _, err := decodeShardMarkerBackend(marker)
	if err != nil {
		return nil, err
	}
	return s.backend(tag)
}

func (s *routedStorage) backend(tag shardBackendTag) (shardBackend, error) {
	if tag == packedObjectBackendTag && s.packed != nil {
		return s.packed, nil
	}
	if s.primary.backendTag() == tag {
		return s.primary, nil
	}
	if s.secondary != nil && s.secondary.backendTag() == tag {
		return s.secondary, nil
	}
	if s.tertiary != nil && s.tertiary.backendTag() == tag {
		return s.tertiary, nil
	}
	if s.quaternary != nil && s.quaternary.backendTag() == tag {
		return s.quaternary, nil
	}
	if s.quinary != nil && s.quinary.backendTag() == tag {
		return s.quinary, nil
	}
	return nil, fmt.Errorf("%w: shard backend 0x%02x is unavailable", ErrStoreIntegrity, tag)
}

func (s *routedStorage) localBackend() (*localBackend, error) {
	if local, ok := s.primary.(*localBackend); ok {
		return local, nil
	}
	if local, ok := s.secondary.(*localBackend); ok {
		return local, nil
	}
	if local, ok := s.tertiary.(*localBackend); ok {
		return local, nil
	}
	if local, ok := s.quaternary.(*localBackend); ok {
		return local, nil
	}
	if local, ok := s.quinary.(*localBackend); ok {
		return local, nil
	}
	return nil, fmt.Errorf("%w: local shard backend is unavailable", ErrStoreIntegrity)
}
