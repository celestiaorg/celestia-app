package fibre

import (
	"context"
	"encoding/binary"
	"log/slog"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/stretchr/testify/require"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestUploadCoordinatorCollidingPromisesReachS3Concurrently(t *testing.T) {
	key := secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey)
	seen := make(map[uint16]*PaymentPromise)
	var promises []*PaymentPromise
	for i := int64(0); len(promises) == 0; i++ {
		p := &PaymentPromise{ChainID: "chain", SignerKey: key, Commitment: generateCommitment(), CreationTimestamp: time.Unix(i, 0), Signature: []byte{1}}
		h, err := p.Hash()
		require.NoError(t, err)
		stripe := binary.BigEndian.Uint16(h) % 2048
		if old := seen[stripe]; old != nil {
			promises = []*PaymentPromise{old, p}
		} else {
			seen[stripe] = p
		}
	}
	store := newMarkerTestStore(t)
	entered := make(chan struct{}, 2)
	unblock := make(chan struct{})
	backend := newObjectBackend(&s3ObjectClientStub{putObject: func(ctx context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
		entered <- struct{}{}
		select {
		case <-unblock:
			return &s3.PutObjectOutput{}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}, objectNamespace{Bucket: "test"})
	store.shards = newRoutedStorage(backend, store.shards.primary)
	shard := &types.BlobShard{Rows: []*types.BlobRow{{Data: []byte("data")}}}
	occ := newOccupancy(shardBinarySize(shard) * 2)
	metrics, err := newServerMetrics(metricnoop.NewMeterProvider().Meter("test"), occ)
	require.NoError(t, err)
	server := &Server{store: store, occ: occ, metrics: metrics, tracer: tracenoop.NewTracerProvider().Tracer("test")}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result := make(chan error, 2)
	for _, p := range promises {
		go func(p *PaymentPromise) {
			h, _ := p.Hash()
			result <- server.storeShard(ctx, slog.Default(), p, h, time.Now().Add(time.Hour), shard)
		}(p)
	}
	for range 2 {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("unrelated promises blocked before S3")
		}
	}
	close(unblock)
	for range 2 {
		require.NoError(t, <-result)
	}
	require.Equal(t, shardBinarySize(shard)*2, occ.usage())
	for _, p := range promises {
		h, _ := p.Hash()
		_, accounted, err := store.uploadShardStatus(t.Context(), p.Commitment, h)
		require.NoError(t, err)
		require.True(t, accounted)
	}
	require.Empty(t, server.uploads.owners)
}
