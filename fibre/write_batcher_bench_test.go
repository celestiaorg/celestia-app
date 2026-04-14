package fibre

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	ds "github.com/ipfs/go-datastore"
)

// BenchmarkStorePut compares batched vs direct writes across block sizes and shard widths.
//
// The cases model a 4096-row block square. Row size is derived from the block size, and
// each shard stores between 148 and 512 rows. Entry counts are scaled down for larger
// blocks to keep each case in roughly the same raw-data budget.
//
// Run with: go test -bench=BenchmarkStorePut -benchtime=3x -run='^$' -timeout=300s
// goos: linux
// goarch: amd64
// pkg: github.com/celestiaorg/celestia-app/v8/fibre
// cpu: Intel(R) Xeon(R) Platinum 8358 CPU @ 2.60GHz
// BenchmarkStorePut/block=1MiB/rows=148/n=4000/batched-16         6     174728631 ns/op   329721865 B/op    45494 allocs/op
// BenchmarkStorePut/block=1MiB/rows=148/n=4000/direct-16          6     197369592 ns/op    69217642 B/op    44928 allocs/op
// BenchmarkStorePut/block=1MiB/rows=256/n=4000/batched-16         4     279857906 ns/op   567433176 B/op    49576 allocs/op
// BenchmarkStorePut/block=1MiB/rows=256/n=4000/direct-16          3     491148407 ns/op   217367277 B/op    57645 allocs/op
// BenchmarkStorePut/block=1MiB/rows=512/n=4000/batched-16         2     507556995 ns/op  1164617760 B/op    65666 allocs/op
// BenchmarkStorePut/block=1MiB/rows=512/n=4000/direct-16          1    1414427620 ns/op  1144028000 B/op    93561 allocs/op
// BenchmarkStorePut/block=4MiB/rows=148/n=4000/batched-16         4     330885234 ns/op   792867618 B/op    50491 allocs/op
// BenchmarkStorePut/block=4MiB/rows=148/n=4000/direct-16          2    1003452128 ns/op   437380408 B/op    73480 allocs/op
// BenchmarkStorePut/block=4MiB/rows=256/n=4000/batched-16         2     606158532 ns/op  1408833788 B/op    67554 allocs/op
// BenchmarkStorePut/block=4MiB/rows=256/n=4000/direct-16          1    2185980690 ns/op  1410671952 B/op   109681 allocs/op
// BenchmarkStorePut/block=4MiB/rows=512/n=4000/batched-16         1    1125925976 ns/op  3012490704 B/op   102445 allocs/op
// BenchmarkStorePut/block=4MiB/rows=512/n=4000/direct-16          1    5146199655 ns/op  2857120320 B/op   177483 allocs/op
// BenchmarkStorePut/block=16MiB/rows=148/n=1000/batched-16        2     589569382 ns/op  1427095288 B/op    36837 allocs/op
// BenchmarkStorePut/block=16MiB/rows=148/n=1000/direct-16         1    2458518810 ns/op  1418953592 B/op    81165 allocs/op
// BenchmarkStorePut/block=16MiB/rows=256/n=1000/batched-16        1    1406333372 ns/op  2614284688 B/op    62757 allocs/op
// BenchmarkStorePut/block=16MiB/rows=256/n=1000/direct-16         1    6865581787 ns/op  2545240968 B/op   171847 allocs/op
// BenchmarkStorePut/block=16MiB/rows=512/n=1000/batched-16        1    1300101694 ns/op  2546529648 B/op   136279 allocs/op
// BenchmarkStorePut/block=16MiB/rows=512/n=1000/direct-16         1    4079247781 ns/op  2774614024 B/op   204635 allocs/op
// BenchmarkStorePut/block=64MiB/rows=148/n=1000/batched-16        1    1563472769 ns/op  2900473640 B/op   155645 allocs/op
// BenchmarkStorePut/block=64MiB/rows=148/n=1000/direct-16         1    4707789880 ns/op  3152937472 B/op   217698 allocs/op
func BenchmarkStorePut(b *testing.B) {
	cases := []struct {
		name     string
		n        int
		rowCount int
		rowSize  int
	}{
		{"block=1MiB/rows=148/n=4000", 4000, 148, 256},
		{"block=1MiB/rows=256/n=4000", 4000, 256, 256},
		{"block=1MiB/rows=512/n=4000", 4000, 512, 256},
		{"block=4MiB/rows=148/n=4000", 4000, 148, 1024},
		{"block=4MiB/rows=256/n=4000", 4000, 256, 1024},
		{"block=4MiB/rows=512/n=4000", 4000, 512, 1024},
		{"block=16MiB/rows=148/n=1000", 2000, 148, 4096},
		{"block=16MiB/rows=256/n=1000", 2000, 256, 4096},
		{"block=16MiB/rows=512/n=1000", 1000, 512, 4096},
		{"block=64MiB/rows=148/n=1000", 1000, 148, 16 * 1024},
		{"block=64MiB/rows=256/n=1000", 1000, 256, 16 * 1024},
	}

	savers := []struct {
		name string
		new  func(ds.Batching) putter
	}{
		{"batched", func(d ds.Batching) putter {
			return newWriteBatcherWithOpts(d, writeBatcherOptions{
				shardCount:    4,
				queueSize:     4096,
				maxPending:    512,
				minBatchBytes: 8 << 20,
				maxBatchBytes: 1 << 30,
				flushInterval: 1 * time.Millisecond,
			})
		}},
		{"direct", func(d ds.Batching) putter { return newDirectPutter(d) }},
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

// BenchmarkStorePutBatcherTuning compares a few plausible batcher presets on
// representative medium and large shard cases.
//
// Run with: go test -bench=BenchmarkStorePutBatcherTuning -benchtime=1x -run='^$'
func BenchmarkStorePutBatcherTuning(b *testing.B) {
	cases := []struct {
		name     string
		n        int
		rowCount int
		rowSize  int
	}{
		{"block=16MiB/rows=256/n=1000", 1000, 256, 4096},
		{"block=64MiB/rows=256/n=256", 256, 256, 16 * 1024},
		{"block=128MiB/rows=512/n=64", 64, 512, 32 * 1024},
	}

	presets := []struct {
		name string
		opts writeBatcherOptions
	}{
		{
			name: "bytes=64MiB/flush=1ms",
			opts: writeBatcherOptions{
				queueSize:     4096,
				maxPending:    512,
				minBatchBytes: 64 << 20,
				maxBatchBytes: 1 << 30,
				flushInterval: 1 * time.Millisecond,
			},
		},
		{
			name: "max=512/bytes=64MiB/flush=1ms",
			opts: writeBatcherOptions{
				queueSize:     4096,
				maxPending:    512,
				minBatchBytes: 64 << 20,
				maxBatchBytes: 1 << 30,
				flushInterval: 1 * time.Millisecond,
			},
		},
		{
			name: "bytes=32MiB/flush=1ms",
			opts: writeBatcherOptions{
				queueSize:     4096,
				maxPending:    512,
				minBatchBytes: 32 << 20,
				maxBatchBytes: 1 << 30,
				flushInterval: 1 * time.Millisecond,
			},
		},
		{
			name: "bytes=128MiB/flush=1ms",
			opts: writeBatcherOptions{
				queueSize:     4096,
				maxPending:    512,
				minBatchBytes: 128 << 20,
				maxBatchBytes: 1 << 30,
				flushInterval: 1 * time.Millisecond,
			},
		},
		{
			name: "bytes=64MiB/flush=2ms",
			opts: writeBatcherOptions{
				queueSize:     4096,
				maxPending:    512,
				minBatchBytes: 64 << 20,
				maxBatchBytes: 1 << 30,
				flushInterval: 2 * time.Millisecond,
			},
		},
	}

	for _, tc := range cases {
		entries := makePutEntries(b, tc.n, tc.rowCount, tc.rowSize)
		for _, preset := range presets {
			b.Run(fmt.Sprintf("%s/%s", tc.name, preset.name), func(b *testing.B) {
				benchStorePut(
					b,
					entries,
					newPebbleBenchStore,
					func(d ds.Batching) putter { return newWriteBatcherWithOpts(d, preset.opts) },
				)
			})
		}
	}
}

type putEntry struct {
	promise *PaymentPromise
	shard   *types.BlobShard
	pruneAt time.Time
}

func benchStorePut(b *testing.B, entries []putEntry, newStore func(*testing.B) *Store, newPutter func(ds.Batching) putter) {
	b.ReportAllocs()

	for b.Loop() {
		b.StopTimer()
		store := newStore(b)
		store.putter.close()
		store.putter = newPutter(store.ds)
		b.StartTimer()

		errCh := make(chan error, len(entries))
		for _, e := range entries {
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
