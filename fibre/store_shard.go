package fibre

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
)

// shardBackend stores shard payloads in one storage backend.
type shardBackend interface {
	backendTag() shardBackendTag
	Put(context.Context, Commitment, []byte, *types.BlobShard) (bool, error)
	Get(context.Context, Commitment, []byte) (*types.BlobShard, error)
	Has(context.Context, Commitment, []byte) (bool, error)
	Delete(context.Context, Commitment, []byte) error
}

// routedStorage writes to its primary backend and routes existing markers by tag.
type routedStorage struct {
	primary   shardBackend
	secondary shardBackend
}

func newRoutedStorage(primary, secondary shardBackend) *routedStorage {
	return &routedStorage{primary: primary, secondary: secondary}
}

func (s *routedStorage) setMetrics(metrics *serverMetrics) {
	for _, backend := range []shardBackend{s.primary, s.secondary} {
		switch backend := backend.(type) {
		case *localBackend:
			backend.metrics = metrics
		case *objectBackend:
			backend.metrics = metrics
		}
	}
}

func (s *routedStorage) marker(size int64) []byte {
	return encodeShardMarkerForBackend(s.primary.backendTag(), size)
}

func (s *routedStorage) Put(ctx context.Context, marker []byte, commitment Commitment, promiseHash []byte, shard *types.BlobShard) (bool, error) {
	backend, err := s.backendForMarker(marker)
	if err != nil {
		return false, err
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
// Local deletes stop on failure. Object requests contain at most 1,000 keys; per-key failures remain for retry.
func (s *routedStorage) DeleteBatch(ctx context.Context, shards []markedShard) ([]int, error) {
	var local, objects []int
	var object *objectBackend
	for i, shard := range shards {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		backend, err := s.backendForMarker(shard.marker)
		if err != nil {
			return nil, err
		}
		if backend.backendTag() == objectBackendTag {
			var ok bool
			object, ok = backend.(*objectBackend)
			if !ok {
				return nil, fmt.Errorf("%w: object backend does not support batch deletion", ErrStoreIntegrity)
			}
			objects = append(objects, i)
		} else {
			local = append(local, i)
		}
	}

	var successful []int
	var deleteErr error
	for _, i := range local {
		shard := shards[i]
		if err := s.Delete(ctx, shard.marker, shard.id.commitment, shard.id.promiseHash); err != nil {
			deleteErr = err
			break
		}
		successful = append(successful, i)
	}
	if len(objects) == 0 {
		return successful, deleteErr
	}
	for indices := range slices.Chunk(objects, maxObjectDeleteBatchSize) {
		ids := make([]shardID, len(indices))
		for i, index := range indices {
			ids[i] = shards[index].id
		}
		results, err := object.DeleteObjects(ctx, ids)
		if err != nil {
			return successful, errors.Join(deleteErr, err)
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
	return successful, deleteErr
}

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
	if s.primary.backendTag() == tag {
		return s.primary, nil
	}
	if s.secondary != nil && s.secondary.backendTag() == tag {
		return s.secondary, nil
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
	return nil, fmt.Errorf("%w: local shard backend is unavailable", ErrStoreIntegrity)
}
