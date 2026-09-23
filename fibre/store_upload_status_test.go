package fibre

import (
	"context"
	"errors"
	"fmt"
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

func TestUploadObjectStatusConditionalPut(t *testing.T) {
	for _, tag := range []shardBackendTag{objectBackendTag, hashedObjectBackendTag, nextHashedObjectBackendTag} {
		t.Run(fmt.Sprint(tag), func(t *testing.T) {
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
			backend.hashFirst = tag != objectBackendTag
			backend.hashedTag = tag
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
			has, accounted, err := store.uploadShardStatus(t.Context(), promise.Commitment, hash)
			require.NoError(t, err)
			require.False(t, has)
			require.False(t, accounted)
			for i := range 3 {
				if i == 1 {
					store.shards.primary, store.shards.secondary = store.shards.secondary, store.shards.primary
				}
				if i == 2 {
					present = false
				} // Repair an object lost while its marker remains.
				require.NoError(t, server.storeShard(t.Context(), slog.Default(), promise, hash, promise.CreationTimestamp, shard))
				require.True(t, present)
				require.Equal(t, size, occ.usage())
				has, accounted, err = store.uploadShardStatus(t.Context(), promise.Commitment, hash)
				require.NoError(t, err)
				require.False(t, has)
				require.True(t, accounted)
			}
			require.Equal(t, 3, puts)
			require.Zero(t, heads)
			present = false
			fail = true
			require.Error(t, server.storeShard(t.Context(), slog.Default(), promise, hash, promise.CreationTimestamp, shard))
			require.Equal(t, size, occ.usage())
			require.Zero(t, heads)
			has, err = store.Has(t.Context(), promise.Commitment, hash)
			require.NoError(t, err)
			require.False(t, has)
			require.Equal(t, 1, heads)
		})
	}
}
