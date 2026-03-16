package fibre

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	ds "github.com/ipfs/go-datastore"
	pebble "github.com/ipfs/go-ds-pebble"
)

// BenchmarkStorePut compares batched vs direct writes across shard sizes and concurrency.
//
// Run with: go test -bench=BenchmarkStorePut -benchtime=3x -run='^$' -timeout=300s
func BenchmarkStorePut(b *testing.B) {
	cases := []struct {
		name     string
		n        int
		rowCount int
		rowSize  int
	}{
		{"32KiB+", 2000, 148, 256},
		{"256KiB+", 2000, 148, 2048},
		{"1Mb+", 2000, 148, 10 * 1024},
	}

	savers := []struct {
		name string
		new  func(ds.Batching) blobSaver
	}{
		{"batched", func(d ds.Batching) blobSaver {
			return newWriteBatcherWithOpts(d, 4096, 512)
		}},
		{"direct", func(d ds.Batching) blobSaver { return newDirectWriter(d) }},
	}

	for _, tc := range cases {
		entries := makePutEntries(b, tc.n, tc.rowCount, tc.rowSize)

		for _, saver := range savers {
			b.Run(fmt.Sprintf("%s/%s", tc.name, saver.name), func(b *testing.B) {
				benchStorePut(b, entries, newPebbleBenchStore, saver.new)
			})
		}
	}
}

// BenchmarkStorePutTuning sweeps Pebble and writeBatcher parameters to find the
// optimal configuration for concurrent store.Put workloads.
//
// Run with: go test -bench=BenchmarkStorePutTuning -benchtime=3x -run='^$' -timeout=600s
func BenchmarkStorePutTuning(b *testing.B) {
	entries := makePutEntries(b, 1000, 148, 2048)

	type pebbleCfg struct {
		name       string
		disableWAL bool
		memTable   uint64 // bytes
		memStop    int
		compConc   [2]int // lower, upper
		l0Stop     int
	}

	type batcherCfg struct {
		name       string
		maxPending int
	}

	pebbleConfigs := []pebbleCfg{
		{"default", false, 16 << 20, 2, [2]int{1, 1}, 12},
		{"mem64M", false, 64 << 20, 2, [2]int{1, 1}, 12},
		{"memStop4", false, 16 << 20, 4, [2]int{1, 1}, 12},
		{"comp4", false, 16 << 20, 2, [2]int{1, 4}, 12},
		{"l0Stop24", false, 16 << 20, 2, [2]int{1, 1}, 24},
	}

	batcherConfigs := []batcherCfg{
		{"mp128", 128},
		{"mp512", 512},
	}

	for _, pc := range pebbleConfigs {
		for _, bc := range batcherConfigs {
			name := fmt.Sprintf("pebble_%s/batcher_%s", pc.name, bc.name)
			pc := pc
			bc := bc
			b.Run(name, func(b *testing.B) {
				newStore := func(b *testing.B) *Store {
					return newTunedPebbleStore(b, pc.disableWAL, pc.memTable, pc.memStop, pc.compConc, pc.l0Stop)
				}
				newSaver := func(d ds.Batching) blobSaver {
					return newWriteBatcherWithOpts(d, 4096, bc.maxPending)
				}
				benchStorePut(b, entries, newStore, newSaver)
			})
		}
	}
}

type putEntry struct {
	promise *PaymentPromise
	shard   *types.BlobShard
	pruneAt time.Time
}

func benchStorePut(b *testing.B, entries []putEntry, newStore func(*testing.B) *Store, newSaver func(ds.Batching) blobSaver) {
	b.ReportAllocs()

	for b.Loop() {
		b.StopTimer()
		store := newStore(b)
		store.saver.close()
		store.saver = newSaver(store.ds)
		b.StartTimer()

		errCh := make(chan error, len(entries))
		for _, e := range entries {
			e := e
			go func() {
				errCh <- store.Put(b.Context(), e.promise, e.shard, e.pruneAt)
			}()
		}

		for range len(entries) {
			if err := <-errCh; err != nil {
				b.Fatal(err)
			}
		}

		b.StopTimer()
		store.Close()
		b.StartTimer()
	}

	b.StopTimer()
}

// --- Store constructors ---

func newPebbleBenchStore(b *testing.B) *Store {
	b.Helper()
	cfg := DefaultStoreConfig()
	cfg.Path = b.TempDir()
	s, err := NewPebbleStore(cfg)
	if err != nil {
		b.Fatal(err)
	}
	return s
}

func newTunedPebbleStore(b *testing.B, disableWAL bool, memTableSize uint64, memTableStop int, compConc [2]int, l0Stop int) *Store {
	b.Helper()

	opts := &pebbledb.Options{
		DisableWAL:                  disableWAL,
		MemTableSize:                memTableSize,
		MemTableStopWritesThreshold: memTableStop,
		L0CompactionThreshold:       4,
		L0StopWritesThreshold:       l0Stop,
		LBaseMaxBytes:               64 << 20,
	}

	lower, upper := compConc[0], compConc[1]
	opts.CompactionConcurrencyRange = func() (int, int) { return lower, upper }

	opts.Experimental.ValueSeparationPolicy = func() pebbledb.ValueSeparationPolicy {
		return pebbledb.ValueSeparationPolicy{
			Enabled:               true,
			MinimumSize:           4096,
			MaxBlobReferenceDepth: 4,
			TargetGarbageRatio:    0.3,
			RewriteMinimumAge:     0,
		}
	}

	cfg := DefaultStoreConfig()
	cfg.Path = b.TempDir()

	pds, err := pebble.NewDatastore(cfg.Path, pebble.WithPebbleOpts(opts))
	if err != nil {
		b.Fatal(err)
	}

	return &Store{
		cfg:   cfg,
		ds:    pds,
		saver: newDirectWriter(pds), // placeholder, benchStorePut swaps it
	}
}

// --- Data generation ---

func makePutEntries(b *testing.B, n, rowCount, rowSize int) []putEntry {
	b.Helper()

	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	signerKey := secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey)
	ns := share.MustNewV0Namespace([]byte("bench"))

	entries := make([]putEntry, n)
	for i := range entries {
		var commitment Commitment
		binary.BigEndian.PutUint64(commitment[:8], uint64(i))

		rows := make([]*types.BlobRow, rowCount)
		for r := range rows {
			rows[r] = &types.BlobRow{
				Index: uint32(r),
				Data:  make([]byte, rowSize),
				Proof: [][]byte{make([]byte, 256)},
			}
		}

		entries[i] = putEntry{
			promise: &PaymentPromise{
				ChainID:           "bench-chain",
				Height:            uint64(i),
				Namespace:         ns,
				UploadSize:        uint32(rowCount * rowSize),
				BlobVersion:       0,
				Commitment:        commitment,
				CreationTimestamp: baseTime.Add(time.Duration(i) * time.Minute),
				SignerKey:         signerKey,
				Signature:         make([]byte, 64),
			},
			shard: &types.BlobShard{
				Rows: rows,
				Rlc:  &types.BlobShard_Root{Root: make([]byte, 32)},
			},
			pruneAt: baseTime.Add(time.Duration(i) * time.Minute),
		}
	}

	return entries
}
