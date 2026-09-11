package fibre

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestPruneObjectPartialFailureRecovery(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	fail := true
	var batches []int
	client := &s3ObjectClientStub{
		deleteObject: func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			return nil, errors.New("expected batch deletion")
		},
		deleteObjects: func(_ context.Context, input *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
			batches = append(batches, len(input.Delete.Objects))
			if fail {
				return &s3.DeleteObjectsOutput{Errors: []s3types.Error{
					{Key: input.Delete.Objects[0].Key, Code: aws.String("AccessDenied")},
					{Key: input.Delete.Objects[1].Key, Code: aws.String("NoSuchKey")},
				}}, nil
			}
			return &s3.DeleteObjectsOutput{}, nil
		},
	}
	store.shards.secondary = newObjectBackend(client, ObjectNamespace{Bucket: "bucket", Prefix: "prefix", ChainID: "chain", ValidatorAddress: "validator"})
	for _, hash := range []byte{1, 2, 3} {
		setPruneEntry(t, store, pruneAt, commitment, []byte{hash}, encodeShardMarkerForBackend(objectBackendTag, 7))
	}
	localHash := []byte{4}
	localSize := writeMarkerTestShard(t, store, commitment, localHash)
	setPruneEntry(t, store, pruneAt, commitment, localHash, encodeShardMarkerForBackend(localBackendTag, localSize))
	occ := newOccupancy(0)
	occ.seed(21 + localSize)
	provider := sdkmetric.NewMeterProvider()
	metrics, err := newServerMetrics(provider.Meter("prune-test"), occ)
	require.NoError(t, err)
	server := &Server{store: store, occ: occ, metrics: metrics, log: slog.Default()}
	server.prune(t.Context())
	require.Equal(t, int64(7), occ.usage())
	for _, hash := range []byte{1, 2, 3, 4} {
		requirePruneEntry(t, store, pruneAt, commitment, []byte{hash}, hash == 1)
	}
	has, err := storeLocalBackend(t, store).Has(t.Context(), commitment, localHash)
	require.NoError(t, err)
	require.False(t, has)
	fail = false
	server.prune(t.Context())
	server.prune(t.Context())
	require.Zero(t, occ.usage())
	require.Equal(t, []int{3, 1}, batches)
	requirePruneEntry(t, store, pruneAt, commitment, []byte{1}, false)
}

func TestPruneObjectRequestFailure(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "request", true: "cancelled"}[cancelled], func(t *testing.T) {
			store := newMarkerTestStore(t)
			commitment := generateCommitment()
			pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
			requestErr := errors.New("request failed")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			client := &s3ObjectClientStub{
				deleteObject: func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
					return nil, errors.New("expected batch deletion")
				},
				deleteObjects: func(context.Context, *s3.DeleteObjectsInput, ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
					if cancelled {
						cancel()
						return nil, ctx.Err()
					}
					return nil, requestErr
				},
			}
			store.shards = newRoutedStorage(newObjectBackend(client, ObjectNamespace{Bucket: "bucket", Prefix: "prefix", ChainID: "chain", ValidatorAddress: "validator"}), store.shards.primary)
			localHash := []byte{2}
			size := writeMarkerTestShard(t, store, commitment, localHash)
			setPruneEntry(t, store, pruneAt, commitment, localHash, nil)
			setPruneEntry(t, store, pruneAt, commitment, []byte{1}, encodeShardMarkerForBackend(objectBackendTag, 7))
			pruned, freed, err := store.PruneBefore(ctx, pruneAt.Add(time.Hour))
			if cancelled {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, requestErr)
			}
			require.Equal(t, 1, pruned)
			require.Equal(t, size, freed)
			requirePruneEntry(t, store, pruneAt, commitment, localHash, false)
			requirePruneEntry(t, store, pruneAt, commitment, []byte{1}, true)
		})
	}
}

func TestPruneObjectBatchLimit(t *testing.T) {
	store := newMarkerTestStore(t)
	commitment := generateCommitment()
	pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	var batches []int
	client := &s3ObjectClientStub{
		deleteObject: func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			return nil, errors.New("expected batch deletion")
		},
		deleteObjects: func(_ context.Context, input *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
			batches = append(batches, len(input.Delete.Objects))
			return &s3.DeleteObjectsOutput{}, nil
		},
	}
	store.shards.secondary = newObjectBackend(client, ObjectNamespace{Bucket: "bucket", Prefix: "prefix", ChainID: "chain", ValidatorAddress: "validator"})
	for i := range maxObjectDeleteBatchSize + 1 {
		hash := binary.BigEndian.AppendUint64(nil, uint64(i))
		setPruneEntry(t, store, pruneAt, commitment, hash, encodeShardMarkerForBackend(objectBackendTag, 1))
	}
	setPruneEntry(t, store, pruneAt.Add(time.Hour), commitment, []byte{255}, encodeShardMarkerForBackend(objectBackendTag, 1))
	for _, want := range []int{maxObjectDeleteBatchSize, 1, 0} {
		pruned, freed, err := store.PruneBefore(t.Context(), pruneAt.Add(time.Hour))
		require.NoError(t, err)
		require.Equal(t, want, pruned)
		require.Equal(t, int64(want), freed)
	}
	require.Equal(t, []int{maxObjectDeleteBatchSize, 1}, batches)
	requirePruneEntry(t, store, pruneAt.Add(time.Hour), commitment, []byte{255}, true)
}

func setPruneEntry(t *testing.T, store *Store, pruneAt time.Time, commitment Commitment, hash, marker []byte) {
	t.Helper()
	require.NoError(t, store.db.Set(shardKey(commitment, hash), marker, pebbledb.NoSync))
	require.NoError(t, store.db.Set(pruneKey(pruneAt, commitment, hash), nil, pebbledb.NoSync))
	require.NoError(t, store.db.Set(promiseKey(hash), []byte("promise"), pebbledb.NoSync))
}

func requirePruneEntry(t *testing.T, store *Store, pruneAt time.Time, commitment Commitment, hash []byte, present bool) {
	t.Helper()
	for _, key := range [][]byte{shardKey(commitment, hash), pruneKey(pruneAt, commitment, hash), promiseKey(hash)} {
		_, closer, err := store.db.Get(key)
		if present {
			require.NoError(t, err)
			require.NoError(t, closer.Close())
		} else {
			require.ErrorIs(t, err, pebbledb.ErrNotFound)
			require.Nil(t, closer)
		}
	}
}
