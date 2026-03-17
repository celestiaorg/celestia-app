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
			return newWriteBatcherWithOpts(d, 4096, 128, 512, 1*time.Millisecond)
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
