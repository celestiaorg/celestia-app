package fibre

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
)

// TestRoutedStorageBackend verifies that markers select the configured backend.
func TestRoutedStorageBackend(t *testing.T) {
	primary := &taggedShardBackend{tag: localBackendTag}
	secondary := &taggedShardBackend{tag: objectBackendTag}
	storage := newRoutedStorage(primary, secondary)

	marker := storage.marker(42)
	tag, size, err := decodeShardMarkerBackend(marker)
	require.NoError(t, err)
	require.Equal(t, localBackendTag, tag)
	require.EqualValues(t, 42, size)

	backend, err := storage.backend(localBackendTag)
	require.NoError(t, err)
	require.Same(t, primary, backend)

	backend, err = storage.backend(objectBackendTag)
	require.NoError(t, err)
	require.Same(t, secondary, backend)
	backend, err = storage.backendForMarker(encodeShardMarkerForBackend(objectBackendTag, 42))
	require.NoError(t, err)
	require.Same(t, secondary, backend)

	_, err = storage.backend(shardBackendTag(3))
	require.ErrorIs(t, err, ErrStoreIntegrity)
}

func TestRoutedStorageWriteMarker(t *testing.T) {
	for _, primary := range []string{"local", "object"} {
		for _, tag := range []shardBackendTag{localBackendTag, objectBackendTag} {
			t.Run(fmt.Sprintf("%s/%d", primary, tag), func(t *testing.T) {
				store := newMarkerTestStore(t)
				cfg := testObjectStorageConfig()
				cfg.ChainID, cfg.ValidatorAddress = "chain", "validator"
				want := namespaceFromConfig(cfg)
				object := newObjectBackend(nil, cfg.Bucket, cfg.Prefix, cfg.ChainID, cfg.ValidatorAddress)
				var err error
				object.namespace, err = json.Marshal(want)
				require.NoError(t, err)
				storage := newRoutedStorage(store.shards.primary, object)
				if primary == "object" {
					storage = newRoutedStorage(object, store.shards.primary)
				}
				commitment, hash := generateCommitment(), []byte{1}
				marker := encodeShardMarkerForBackend(tag, 42)
				batch := store.db.NewBatch()
				defer batch.Close()
				require.NoError(t, storage.writeMarker(batch, commitment, hash, marker))
				for _, key := range [][]byte{shardKey(commitment, hash), []byte(objectNamespaceKey)} {
					_, _, err := store.db.Get(key)
					require.ErrorIs(t, err, pebbledb.ErrNotFound, "metadata must wait for the batch commit")
				}
				require.NoError(t, batch.Commit(pebbledb.Sync))
				data, closer, err := store.db.Get(shardKey(commitment, hash))
				require.NoError(t, err)
				require.Equal(t, marker, data)
				require.NoError(t, closer.Close())
				got, recorded, err := readObjectNamespace(store.db)
				require.NoError(t, err)
				require.Equal(t, tag == objectBackendTag, recorded)
				if recorded {
					require.Equal(t, want, got)
				}
			})
		}
	}
}

type taggedShardBackend struct {
	shardBackend
	tag shardBackendTag
}

func (b *taggedShardBackend) backendTag() shardBackendTag {
	return b.tag
}

func TestRoutedStorageDeleteBatchLocal(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	hash := []byte{1}
	size := writeMarkerTestShard(t, store, commitment, hash)
	shards := []markedShard{
		{id: shardID{commitment: commitment, promiseHash: hash}, marker: encodeShardMarkerForBackend(localBackendTag, size)},
		{id: shardID{commitment: commitment, promiseHash: []byte{2}}},
	}
	successful, err := store.shards.DeleteBatch(t.Context(), shards)
	require.NoError(t, err)
	require.Equal(t, []int{0, 1}, successful)
	has, err := storeLocalBackend(t, store).Has(t.Context(), commitment, hash)
	require.NoError(t, err)
	require.False(t, has)
}

func TestRoutedStorageChunksObjectDeletes(t *testing.T) {
	for _, outcome := range []string{"success", "per-key failure", "request failure", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			store := newMarkerTestStore(t)
			commitment := generateCommitment()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			requestErr := errors.New("request failed")
			var batches []int
			object := newObjectBackend(&s3ObjectClientStub{
				deleteObjects: func(_ context.Context, input *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
					batches = append(batches, len(input.Delete.Objects))
					if len(batches) == 1 {
						switch outcome {
						case "per-key failure":
							return &s3.DeleteObjectsOutput{Errors: []s3types.Error{{
								Key: input.Delete.Objects[0].Key, Code: aws.String("AccessDenied"),
							}}}, nil
						case "cancelled":
							cancel()
						}
					} else if outcome == "request failure" {
						return nil, requestErr
					}
					return &s3.DeleteObjectsOutput{}, nil
				},
			}, "bucket", "prefix", "chain", "validator")
			store.shards.secondary = object
			shards := make([]markedShard, maxObjectDeleteBatchSize+2)
			localIndex := maxObjectDeleteBatchSize / 2
			want := []int{localIndex}
			for i := range shards {
				hash := binary.BigEndian.AppendUint64(nil, uint64(i))
				shards[i] = markedShard{
					id:     shardID{commitment: commitment, promiseHash: hash},
					marker: encodeShardMarkerForBackend(objectBackendTag, 1),
				}
				if i == localIndex {
					size := writeMarkerTestShard(t, store, commitment, hash)
					shards[i].marker = encodeShardMarkerForBackend(localBackendTag, size)
					continue
				}
				want = append(want, i)
			}
			successful, err := store.shards.DeleteBatch(ctx, shards)
			switch outcome {
			case "success":
				require.NoError(t, err)
			case "per-key failure":
				require.ErrorContains(t, err, "AccessDenied")
				want = slices.Delete(want, 1, 2)
			case "request failure":
				require.ErrorIs(t, err, requestErr)
				want = want[:len(want)-1]
			case "cancelled":
				require.ErrorIs(t, err, context.Canceled)
				want = want[:len(want)-1]
			}
			require.Equal(t, want, successful)
			if outcome == "cancelled" {
				require.Equal(t, []int{maxObjectDeleteBatchSize}, batches)
			} else {
				require.Equal(t, []int{maxObjectDeleteBatchSize, 1}, batches)
			}
			has, err := storeLocalBackend(t, store).Has(t.Context(), commitment, shards[localIndex].id.promiseHash)
			require.NoError(t, err)
			require.False(t, has)
		})
	}
}
