package fibre

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestServerPruneContinuesAfterLocalFailure(t *testing.T) {
	for _, failures := range []int{1, maxPruneBatchSize} {
		t.Run(fmt.Sprint(failures), func(t *testing.T) {
			store := newMarkerTestStore(t)
			local := storeLocalBackend(t, store)
			backend := &failingDeleteBackend{shardBackend: local, failures: failures, attempts: make(map[uint64]int)}
			commitment := generateCommitment()
			pruneAt := time.Now().Add(-time.Hour)
			const total = maxPruneBatchSize + 2
			var size int64
			for i := range total {
				hash := binary.BigEndian.AppendUint64(nil, uint64(i))
				size = writeMarkerTestShard(t, store, commitment, hash)
				setPruneEntry(t, store, pruneAt, commitment, hash, encodeShardMarkerForBackend(localBackendTag, size))
			}
			futureHash := binary.BigEndian.AppendUint64(nil, total)
			writeMarkerTestShard(t, store, commitment, futureHash)
			setPruneEntry(t, store, pruneAt.Add(2*time.Hour), commitment, futureHash, encodeShardMarkerForBackend(localBackendTag, size))
			store.shards.primary = backend
			occ := newOccupancy(0)
			occ.seed((total + 1) * size)
			provider := sdkmetric.NewMeterProvider()
			metrics, err := newServerMetrics(provider.Meter("prune-test"), occ)
			require.NoError(t, err)
			var logs strings.Builder
			server := &Server{store: store, occ: occ, metrics: metrics, log: slog.New(slog.NewTextHandler(&logs, nil))}
			for pass := 1; pass <= 2; pass++ {
				server.prune(t.Context())
				require.Equal(t, int64(failures+1)*size, occ.usage())
				for i := range total {
					hash := binary.BigEndian.AppendUint64(nil, uint64(i))
					requirePruneEntry(t, store, pruneAt, commitment, hash, i < failures)
					has, err := local.Has(t.Context(), commitment, hash)
					require.NoError(t, err)
					require.Equal(t, i < failures, has)
					wantAttempts := 1
					if i < failures {
						wantAttempts = pass
					}
					require.Equal(t, wantAttempts, backend.attempts[uint64(i)])
				}
			}
			require.Contains(t, logs.String(), "prune retained failed payload deletions")
			require.Zero(t, backend.attempts[total])
			requirePruneEntry(t, store, pruneAt.Add(2*time.Hour), commitment, futureHash, true)
			backend.failures = 0
			server.prune(t.Context())
			server.prune(t.Context())
			require.Equal(t, size, occ.usage())
			for i := range failures {
				hash := binary.BigEndian.AppendUint64(nil, uint64(i))
				requirePruneEntry(t, store, pruneAt, commitment, hash, false)
				require.Equal(t, 3, backend.attempts[uint64(i)])
			}
		})
	}
}

func TestServerPruneLocalFailureWithObjectRequest(t *testing.T) {
	for _, requestFails := range []bool{false, true} {
		t.Run(fmt.Sprint(requestFails), func(t *testing.T) {
			store := newMarkerTestStore(t)
			commitment := generateCommitment()
			pruneAt := time.Now().Add(-time.Hour)
			backend := &failingDeleteBackend{shardBackend: store.shards.primary, failures: 1, attempts: make(map[uint64]int)}
			store.shards.primary = backend
			requests := 0
			store.shards.secondary = newObjectBackend(&s3ObjectClientStub{
				deleteObjects: func(context.Context, *s3.DeleteObjectsInput, ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
					requests++
					if requestFails {
						return nil, errors.New("request failed")
					}
					return &s3.DeleteObjectsOutput{}, nil
				},
			}, "bucket", "prefix", "chain", "validator")
			for i := range maxPruneBatchSize + 1 {
				tag := localBackendTag
				if i == 1 {
					tag = objectBackendTag
				}
				hash := binary.BigEndian.AppendUint64(nil, uint64(i))
				setPruneEntry(t, store, pruneAt, commitment, hash, encodeShardMarkerForBackend(tag, 1))
			}
			occ := newOccupancy(0)
			occ.seed(maxPruneBatchSize + 1)
			provider := sdkmetric.NewMeterProvider()
			metrics, err := newServerMetrics(provider.Meter("prune-test"), occ)
			require.NoError(t, err)
			var logs strings.Builder
			server := &Server{store: store, occ: occ, metrics: metrics, log: slog.New(slog.NewTextHandler(&logs, nil))}
			server.prune(t.Context())
			require.Equal(t, 1, requests)
			require.Equal(t, 1, backend.attempts[0])
			if requestFails {
				require.Equal(t, int64(3), occ.usage())
				require.Zero(t, backend.attempts[maxPruneBatchSize])
				require.Contains(t, logs.String(), "failed to prune store")
			} else {
				require.Equal(t, int64(1), occ.usage())
				require.Equal(t, 1, backend.attempts[maxPruneBatchSize])
			}
			for i := range maxPruneBatchSize + 1 {
				hash := binary.BigEndian.AppendUint64(nil, uint64(i))
				retained := i == 0 || (requestFails && (i == 1 || i == maxPruneBatchSize))
				requirePruneEntry(t, store, pruneAt, commitment, hash, retained)
			}
		})
	}
}

type failingDeleteBackend struct {
	shardBackend
	failures int
	attempts map[uint64]int
}

func (b *failingDeleteBackend) Delete(ctx context.Context, commitment Commitment, hash []byte) error {
	id := binary.BigEndian.Uint64(hash)
	b.attempts[id]++
	if id < uint64(b.failures) {
		return os.ErrPermission
	}
	return b.shardBackend.Delete(ctx, commitment, hash)
}
