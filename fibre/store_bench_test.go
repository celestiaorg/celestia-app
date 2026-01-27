package fibre_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app-fibre/v6/fibre"
	"github.com/celestiaorg/celestia-app-fibre/v6/x/fibre/types"
	"github.com/stretchr/testify/require"
)

// NOTE: BenchmarkPruneBefore helped to iterate over the few implementations learning the internals of the database
// until the optimal and scalable one was found.
func BenchmarkPruneBefore(b *testing.B) {
	for _, tc := range []struct {
		name    string
		entries int
		prune   int // percent
	}{
		{"100_10pct", 100, 10},
		{"100_50pct", 100, 50},
		{"1000_10pct", 1000, 10},
		{"1000_50pct", 1000, 50},
	} {
		b.Run(tc.name, func(b *testing.B) {
			benchmarkPruneBefore(b, tc.entries, tc.prune)
		})
	}
}

func benchmarkPruneBefore(b *testing.B, totalEntries, prunePercent int) {
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// pre-generate test data
	type entry struct {
		promise *fibre.PaymentPromise
		shard   *types.BlobShard
		pruneAt time.Time
	}
	entries := make([]entry, totalEntries)
	for i := range entries {
		blob := makeBenchBlob(i)
		entries[i] = entry{
			promise: makeTestPaymentPromise(uint64(i), blob.Commitment()),
			shard:   makeBenchShard(blob),
			pruneAt: baseTime.Add(time.Duration(i) * time.Minute),
		}
	}

	cutoffMinute := (totalEntries * prunePercent) / 100
	cutoffTime := baseTime.Add(time.Duration(cutoffMinute) * time.Minute)

	for b.Loop() {
		b.StopTimer()
		store := makeBenchStore(b)
		for _, e := range entries {
			_ = store.Put(b.Context(), e.promise, e.shard, e.pruneAt)
		}
		b.StartTimer()

		pruned, err := store.PruneBefore(b.Context(), cutoffTime)
		require.NoError(b, err)

		b.StopTimer()
		if pruned != cutoffMinute {
			b.Fatalf("expected %d pruned, got %d", cutoffMinute, pruned)
		}
		store.Close()
		b.StartTimer()
	}
}

func makeBenchStore(b *testing.B) *fibre.Store {
	cfg := fibre.DefaultStoreConfig()
	cfg.Path = b.TempDir()
	store, _ := fibre.NewBadgerStore(cfg)
	return store
}

func makeBenchBlob(seed int) *fibre.Blob {
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte((seed + i) % 256)
	}
	blob, _ := fibre.NewBlob(data, fibre.DefaultBlobConfigV0())
	return blob
}

func makeBenchShard(blob *fibre.Blob) *types.BlobShard {
	row0, _ := blob.Row(0)
	row1, _ := blob.Row(1)
	return &types.BlobShard{
		Rows: []*types.BlobRow{
			{Index: 0, Data: row0.Row, Proof: row0.RowProof.RowProof},
			{Index: 1, Data: row1.Row, Proof: row1.RowProof.RowProof},
		},
		Rlc: &types.BlobShard_Root{Root: make([]byte, 32)},
	}
}
