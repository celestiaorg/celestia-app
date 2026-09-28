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
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
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
			metrics, err := newServerMetrics(noop.NewMeterProvider().Meter("prune-test"), occ)
			require.NoError(t, err)
			server := &Server{store: store, occ: occ, metrics: metrics, log: slog.Default()}
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

// TestServerPruneLocalFailureWithObjectRequest verifies that local and object deletion failures do not block later batches.
// It also checks that cancellation stops the pass and retries release occupancy only after successful deletion.
func TestServerPruneLocalFailureWithObjectRequest(t *testing.T) {
	for _, outcome := range []string{"success", "request failure", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			store := newMarkerTestStore(t)
			commitment := generateCommitment()
			pruneAt := time.Now().Add(-time.Hour)
			backend := &failingDeleteBackend{shardBackend: store.shards.primary, failures: 1, attempts: make(map[uint64]int)}
			store.shards.primary = backend
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			requests := 0
			store.shards.secondary = newObjectBackend(&s3ObjectClientStub{
				deleteObjects: func(context.Context, *s3.DeleteObjectsInput, ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
					requests++
					if outcome == "cancelled" {
						cancel()
						return nil, ctx.Err()
					}
					if outcome == "request failure" {
						return nil, errors.New("request failed")
					}
					return &s3.DeleteObjectsOutput{}, nil
				},
			}, objectNamespace{Bucket: "bucket", Prefix: "prefix", ChainID: "chain", ValidatorAddress: "validator"})
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
			metrics, err := newServerMetrics(noop.NewMeterProvider().Meter("prune-test"), occ)
			require.NoError(t, err)
			server := &Server{store: store, occ: occ, metrics: metrics, log: slog.Default()}
			server.prune(ctx)
			require.Equal(t, 1, requests)
			require.Equal(t, 1, backend.attempts[0])
			if outcome == "cancelled" {
				require.Equal(t, int64(3), occ.usage())
				require.Zero(t, backend.attempts[maxPruneBatchSize])
			} else {
				want := int64(1)
				if outcome == "request failure" {
					want++
				}
				require.Equal(t, want, occ.usage())
				require.Equal(t, 1, backend.attempts[maxPruneBatchSize])
			}
			for i := range maxPruneBatchSize + 1 {
				hash := binary.BigEndian.AppendUint64(nil, uint64(i))
				retained := i == 0 || (outcome != "success" && i == 1) || (outcome == "cancelled" && i == maxPruneBatchSize)
				requirePruneEntry(t, store, pruneAt, commitment, hash, retained)
			}
			if outcome == "request failure" {
				outcome = "success"
				server.prune(t.Context())
				server.prune(t.Context())
				require.Equal(t, int64(1), occ.usage())
				require.Equal(t, 2, requests)
				requirePruneEntry(t, store, pruneAt, commitment, binary.BigEndian.AppendUint64(nil, 1), false)
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

func TestPruneReportsCorruptionWithDeletionFailure(t *testing.T) {
	for _, throughServer := range []bool{false, true} {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("server=%t/cancelled=%t", throughServer, cancelled), func(t *testing.T) {
				store := newMarkerTestStore(t)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				commitment := generateCommitment()
				pruneAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
				malformed := []byte("/prune/202401010000/bad")
				require.NoError(t, store.db.Set(malformed, nil, pebbledb.NoSync))
				for i := range maxPruneBatchSize + 1 {
					hash := binary.BigEndian.AppendUint64(nil, uint64(i))
					setPruneEntry(t, store, pruneAt, commitment, hash, encodeShardMarkerForBackend(localBackendTag, 1))
				}
				backend := &failingDeleteBackend{shardBackend: store.shards.primary, failures: 1, attempts: make(map[uint64]int)}
				store.shards.primary = backend
				if cancelled {
					store.shards.primary = &cancelAfterDeleteBackend{shardBackend: backend, cancel: cancel}
				}
				if throughServer {
					occ := newOccupancy(0)
					occ.seed(maxPruneBatchSize + 1)
					metrics, err := newServerMetrics(noop.NewMeterProvider().Meter("prune-test"), occ)
					require.NoError(t, err)
					var logs strings.Builder
					server := &Server{store: store, occ: occ, metrics: metrics, log: slog.New(slog.NewTextHandler(&logs, nil))}
					server.prune(ctx)
					require.Contains(t, logs.String(), "malformed prune key")
					if cancelled {
						require.Contains(t, logs.String(), "level=ERROR")
						require.Contains(t, logs.String(), "context canceled")
						require.Zero(t, backend.attempts[maxPruneBatchSize])
					} else {
						require.Contains(t, logs.String(), "prune retained failed payload deletions")
						require.NotContains(t, logs.String(), "level=ERROR")
						require.Equal(t, 1, backend.attempts[maxPruneBatchSize])
						require.EqualValues(t, 1, occ.usage())
					}
				} else {
					_, _, err := store.PruneBefore(ctx, pruneAt.Add(time.Hour))
					require.ErrorIs(t, err, ErrStoreIntegrity)
					require.ErrorIs(t, err, os.ErrPermission)
					if cancelled {
						require.ErrorIs(t, err, context.Canceled)
					}
				}
				_, _, err := store.db.Get(malformed)
				require.ErrorIs(t, err, pebbledb.ErrNotFound)
			})
		}
	}
}
