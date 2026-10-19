package fibre

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// TestUploadShardStatusConditionalPut verifies the upload path never issues a
// HEAD against the object backend: uploadShardStatus leaves object presence
// unconfirmed and storeShard relies on the conditional PUT (IfNoneMatch: "*").
// Duplicate uploads stay idempotent because the backend treats a
// PreconditionFailed response as success, and the non-upload Has still HEADs.
func TestUploadShardStatusConditionalPut(t *testing.T) {
	store := newMarkerTestStore(t)
	present, fail := false, false
	puts, heads := 0, 0
	backend := newObjectBackend(&s3ObjectClientStub{
		putObject: func(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			puts++
			require.Equal(t, "*", aws.ToString(in.IfNoneMatch))
			require.Equal(t, "retained", aws.ToString(in.Bucket))
			if fail {
				return nil, errors.New("write failed")
			}
			if present {
				return nil, &smithy.GenericAPIError{Code: "PreconditionFailed"}
			}
			present = true
			return &s3.PutObjectOutput{}, nil
		},
		headObject: func(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			heads++
			if !present {
				return nil, &smithy.GenericAPIError{Code: "NotFound"}
			}
			return &s3.HeadObjectOutput{}, nil
		},
	}, objectNamespace{Bucket: "retained"})
	// Route the object backend as primary so Put writes an object marker, while
	// keeping the memory store's local backend reachable as the secondary.
	store.shards = newRoutedStorage(backend, store.shards.primary)

	promise := &PaymentPromise{ChainID: "chain", SignerKey: secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey), Commitment: generateCommitment(), CreationTimestamp: time.Unix(1, 0), Signature: []byte{1}}
	hash, err := promise.Hash()
	require.NoError(t, err)
	shard := &types.BlobShard{Rows: []*types.BlobRow{{Data: []byte("data")}}}
	size := shardBinarySize(shard)
	occ := newOccupancy(size)
	metrics, err := newServerMetrics(metricnoop.NewMeterProvider().Meter("test"), occ)
	require.NoError(t, err)
	server := &Server{store: store, occ: occ, metrics: metrics, tracer: tracenoop.NewTracerProvider().Tracer("test")}

	// Before any marker exists the upload check reports absent without a HEAD.
	has, accounted, err := store.uploadShardStatus(t.Context(), promise.Commitment, hash)
	require.NoError(t, err)
	require.False(t, has)
	require.False(t, accounted)

	for i := range 3 {
		if i == 2 {
			present = false // Repair an object lost while its marker remains.
		}
		require.NoError(t, server.storeShard(t.Context(), slog.Default(), promise, hash, promise.CreationTimestamp, shard))
		require.True(t, present)
		require.Equal(t, size, occ.usage())
		has, accounted, err = store.uploadShardStatus(t.Context(), promise.Commitment, hash)
		require.NoError(t, err)
		require.False(t, has)      // Object presence stays unconfirmed for the conditional PUT.
		require.True(t, accounted) // The marker still counts towards occupancy.
	}
	require.Equal(t, 3, puts) // first store, idempotent re-put, and a repair.
	require.Zero(t, heads)

	// An object marker in the secondary slot is checked with a HEAD, because an
	// upload would PUT to the primary backend instead of the object's backend.
	store.shards.primary, store.shards.secondary = store.shards.secondary, store.shards.primary
	has, accounted, err = store.uploadShardStatus(t.Context(), promise.Commitment, hash)
	require.NoError(t, err)
	require.True(t, has)
	require.True(t, accounted)
	require.Equal(t, 1, heads)
	store.shards.primary, store.shards.secondary = store.shards.secondary, store.shards.primary
	heads = 0

	// A failed conditional PUT surfaces the error without a HEAD, and leaves
	// occupancy untouched because the marker was already accounted.
	present = false
	fail = true
	require.Error(t, server.storeShard(t.Context(), slog.Default(), promise, hash, promise.CreationTimestamp, shard))
	require.Equal(t, size, occ.usage())
	require.Zero(t, heads)

	// The non-upload Has still confirms object presence with a HEAD.
	has, err = store.Has(t.Context(), promise.Commitment, hash)
	require.NoError(t, err)
	require.False(t, has)
	require.Equal(t, 1, heads)
}

// TestUploadShardAfterBackendSwitch verifies that retrying an object-backed
// shard after switching the primary backend to local keeps the object marker,
// so pruning still deletes the original object.
func TestUploadShardAfterBackendSwitch(t *testing.T) {
	store := newMarkerTestStore(t)
	present := false
	puts, deletes := 0, 0
	object := newObjectBackend(&s3ObjectClientStub{
		putObject: func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			puts++
			present = true
			return &s3.PutObjectOutput{}, nil
		},
		headObject: func(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			if !present {
				return nil, &smithy.GenericAPIError{Code: "NotFound"}
			}
			return &s3.HeadObjectOutput{}, nil
		},
		deleteObjects: func(_ context.Context, in *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
			deletes += len(in.Delete.Objects)
			present = false
			return &s3.DeleteObjectsOutput{}, nil
		},
	}, objectNamespace{Bucket: "retained"})
	local := store.shards.primary
	store.shards = newRoutedStorage(object, local)

	promise := &PaymentPromise{ChainID: "chain", SignerKey: secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey), Commitment: generateCommitment(), CreationTimestamp: time.Unix(1, 0), Signature: []byte{1}}
	hash, err := promise.Hash()
	require.NoError(t, err)
	shard := &types.BlobShard{Rows: []*types.BlobRow{{Data: []byte("data")}}}
	occ := newOccupancy(shardBinarySize(shard))
	metrics, err := newServerMetrics(metricnoop.NewMeterProvider().Meter("test"), occ)
	require.NoError(t, err)
	server := &Server{store: store, occ: occ, metrics: metrics, tracer: tracenoop.NewTracerProvider().Tracer("test")}

	require.NoError(t, server.storeShard(t.Context(), slog.Default(), promise, hash, promise.CreationTimestamp, shard))
	require.Equal(t, 1, puts)

	// Switch to local storage and retry the same upload.
	store.shards = newRoutedStorage(local, object)
	require.NoError(t, server.storeShard(t.Context(), slog.Default(), promise, hash, promise.CreationTimestamp, shard))
	require.Equal(t, 1, puts)
	localHas, err := local.Has(t.Context(), promise.Commitment, hash)
	require.NoError(t, err)
	require.False(t, localHas, "retry must not copy the shard to local storage")

	pruned, _, err := store.PruneBefore(t.Context(), promise.CreationTimestamp.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, 1, pruned)
	require.Equal(t, 1, deletes)
	require.False(t, present)
}
