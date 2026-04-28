package fibre_test

import (
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/fibre"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
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
			promise: makeTestPaymentPromise(uint64(i), blob.ID()),
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
	store, err := fibre.NewStore(cfg)
	if err != nil {
		b.Fatalf("failed to create store: %v", err)
	}
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
		Root: make([]byte, 32),
	}
}

// BenchmarkStoreWriteRows measures sequential row write performance across blob sizes.
// Reports rows/sec, goodput (raw data throughput), and total throughput.
//
// Run with: go test -bench=BenchmarkStoreWriteRows$ -benchmem -benchtime=3x -run=^$
//
// CPU: AMD Ryzen 9 7940HS w/ Radeon 780M Graphics
// Results (100 validators, 148 rows/shard, Badger with ValueThreshold=1KB):
//
//	Blob Size    Row Size    Rows/sec    Goodput      Throughput    Memory/op
//	1 KiB        64 B        1.09M       0.07 MiB/s   81 MiB/s      17 MB
//	16 KiB       64 B        1.26M       1.3 MiB/s    94 MiB/s      17 MB
//	128 KiB      64 B        1.38M       12 MiB/s     103 MiB/s     17 MB
//	1 MiB        320 B       1.15M       78 MiB/s     366 MiB/s     25 MB
//	16 MiB       4160 B      182K        197 MiB/s    726 MiB/s     138 MB
//	128 MiB      32768 B     25K         219 MiB/s    790 MiB/s     985 MB
//
// Key observations:
//   - Small blobs have high rows/sec but low goodput due to fixed overhead (proofs, min row size)
//   - Peak goodput at 128 MiB: ~219 MiB/s sequential
//   - Overhead ratio (throughput/goodput) decreases with blob size: ~1100x at 1 KiB → ~3.6x at 128 MiB
func BenchmarkStoreWriteRows(b *testing.B) {
	const validators = 100
	params := fibre.DefaultProtocolParams

	// Test blob sizes across orders of magnitude
	blobSizes := []struct {
		name string
		size int
	}{
		{"1_KiB", 1 << 10},
		{"16_KiB", 16 << 10},
		{"128_KiB", 128 << 10},
		{"1_MiB", 1 << 20},
		{"16_MiB", 16 << 20},
		{"128_MiB", 128 << 20},
	}

	for _, bs := range blobSizes {
		b.Run(bs.name, func(b *testing.B) {
			benchmarkStoreWriteRows(b, params, validators, bs.size)
		})
	}
}

// BenchmarkStoreWriteRowsConcurrent measures concurrent row write performance.
// Tests different blob sizes with varying concurrency levels.
//
// Run with: go test -bench=BenchmarkStoreWriteRowsConcurrent -benchmem -benchtime=3x -run=^$
//
// CPU: AMD Ryzen 9 7940HS w/ Radeon 780M Graphics
// Results (100 validators, 148 rows/shard, Badger with ValueThreshold=1KB):
//
//	Blob Size    Concurrency    Goodput      Throughput    Memory/op
//	1 MiB        2              67 MiB/s     316 MiB/s     25 MB
//	1 MiB        4              101 MiB/s    477 MiB/s     20 MB
//	1 MiB        8              116 MiB/s    547 MiB/s     17 MB
//	1 MiB        16             123 MiB/s    579 MiB/s     15 MB
//	16 MiB       2              242 MiB/s    890 MiB/s     136 MB
//	16 MiB       4              300 MiB/s    1104 MiB/s    106 MB
//	16 MiB       8              362 MiB/s    1332 MiB/s    87 MB
//	16 MiB       16             378 MiB/s    1391 MiB/s    82 MB
//	128 MiB      2              231 MiB/s    836 MiB/s     977 MB
//	128 MiB      4              288 MiB/s    1040 MiB/s    739 MB
//	128 MiB      8              312 MiB/s    1126 MiB/s    621 MB
//	128 MiB      16             313 MiB/s    1133 MiB/s    562 MB
//
// Key observations:
//   - Peak goodput at 16 MiB with 16 concurrent writers: ~378 MiB/s (+92% vs sequential)
//   - Peak goodput at 128 MiB with 8-16 concurrent writers: ~313 MiB/s (+43% vs sequential)
//   - Concurrency helps most for medium/large blobs; diminishing returns beyond 8 workers
//   - Memory usage decreases with higher concurrency (better batching)
func BenchmarkStoreWriteRowsConcurrent(b *testing.B) {
	const validators = 100
	params := fibre.DefaultProtocolParams

	blobSizes := []struct {
		name string
		size int
	}{
		{"1_MiB", 1 << 20},
		{"16_MiB", 16 << 20},
		{"128_MiB", 128 << 20},
	}

	concurrencyLevels := []int{2, 4, 8, 16}

	for _, bs := range blobSizes {
		for _, conc := range concurrencyLevels {
			name := fmt.Sprintf("%s/conc_%d", bs.name, conc)
			b.Run(name, func(b *testing.B) {
				benchmarkStoreWriteRowsConcurrent(b, params, validators, bs.size, conc)
			})
		}
	}
}

func benchmarkStoreWriteRows(b *testing.B, params fibre.ProtocolParams, validators int, blobSize int) {
	cfg := fibre.NewBlobConfigFromParams(0, params)

	dataSize := min(blobSize, cfg.MaxDataSize)

	data := make([]byte, dataSize)
	_, err := rand.Read(data)
	require.NoError(b, err)

	blob, err := fibre.NewBlob(data, cfg)
	require.NoError(b, err)

	rowsPerShard := params.MinRowsPerValidator()
	totalRows := params.TotalRows()
	rowSize := blob.RowSize()

	b.Logf("DataSize: %d KiB, RowSize: %d bytes, RowsPerShard: %d, TotalDistributed: %d",
		dataSize/1024, rowSize, rowsPerShard, rowsPerShard*validators)

	type shardEntry struct {
		promise *fibre.PaymentPromise
		shard   *types.BlobShard
		pruneAt time.Time
	}

	shards := make([]shardEntry, validators)
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	for v := range validators {
		rows := make([]*types.BlobRow, 0, rowsPerShard)
		startIdx := v * rowsPerShard
		for r := 0; r < rowsPerShard && startIdx+r < totalRows; r++ {
			rowIdx := startIdx + r
			rowProof, err := blob.Row(rowIdx)
			require.NoError(b, err)
			rows = append(rows, &types.BlobRow{
				Index: uint32(rowIdx),
				Data:  rowProof.Row,
				Proof: rowProof.RowProof.RowProof,
			})
		}

		shards[v] = shardEntry{
			promise: makeTestPaymentPromise(uint64(v), blob.ID()),
			shard: &types.BlobShard{
				Rows: rows,
				Root: make([]byte, 32),
			},
			pruneAt: baseTime.Add(time.Duration(v) * time.Minute),
		}
	}

	// Calculate sizes for reporting
	totalRowsWritten := 0
	totalBytesWritten := 0
	for _, s := range shards {
		totalRowsWritten += len(s.shard.Rows)
		for _, row := range s.shard.Rows {
			totalBytesWritten += len(row.Data) + len(row.Proof)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		b.StopTimer()
		store := makeBenchStore(b)
		b.StartTimer()

		for _, s := range shards {
			err := store.Put(b.Context(), s.promise, s.shard, s.pruneAt)
			if err != nil {
				b.Fatal(err)
			}
		}

		b.StopTimer()
		store.Close()
		b.StartTimer()
	}

	b.StopTimer()

	elapsed := b.Elapsed()
	iterations := float64(b.N)
	totalOps := iterations * float64(validators)

	rowsPerSec := (iterations * float64(totalRowsWritten)) / elapsed.Seconds()
	goodputMBps := (iterations * float64(dataSize)) / elapsed.Seconds() / (1 << 20)
	throughputMBps := (iterations * float64(totalBytesWritten)) / elapsed.Seconds() / (1 << 20)

	b.ReportMetric(rowsPerSec, "rows/sec")
	b.ReportMetric(goodputMBps, "goodput-MiB/s")
	b.ReportMetric(throughputMBps, "throughput-MiB/s")
	b.ReportMetric(totalOps/elapsed.Seconds(), "shards/sec")

	fmt.Printf("\n=== Store Write Performance ===\n")
	fmt.Printf("Data size: %d MiB, Row size: %d bytes\n", dataSize/(1<<20), rowSize)
	fmt.Printf("Rows written: %d, Total bytes: %d MiB\n", totalRowsWritten, totalBytesWritten/(1<<20))
	fmt.Printf("Rows/sec: %.0f\n", rowsPerSec)
	fmt.Printf("Goodput: %.2f MiB/s (raw data)\n", goodputMBps)
	fmt.Printf("Throughput: %.2f MiB/s (with encoding)\n", throughputMBps)
	fmt.Printf("Shards/sec: %.0f\n", totalOps/elapsed.Seconds())
}

func benchmarkStoreWriteRowsConcurrent(b *testing.B, params fibre.ProtocolParams, validators int, blobSize int, concurrency int) {
	cfg := fibre.NewBlobConfigFromParams(0, params)

	dataSize := min(blobSize, cfg.MaxDataSize)

	data := make([]byte, dataSize)
	_, err := rand.Read(data)
	require.NoError(b, err)

	blob, err := fibre.NewBlob(data, cfg)
	require.NoError(b, err)

	rowsPerShard := params.MinRowsPerValidator()
	totalRows := params.TotalRows()
	rowSize := blob.RowSize()

	b.Logf("DataSize: %d KiB, RowSize: %d bytes, RowsPerShard: %d, Concurrency: %d",
		dataSize/1024, rowSize, rowsPerShard, concurrency)

	type shardEntry struct {
		promise *fibre.PaymentPromise
		shard   *types.BlobShard
		pruneAt time.Time
	}

	shards := make([]shardEntry, validators)
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	for v := range validators {
		rows := make([]*types.BlobRow, 0, rowsPerShard)
		startIdx := v * rowsPerShard
		for r := 0; r < rowsPerShard && startIdx+r < totalRows; r++ {
			rowIdx := startIdx + r
			rowProof, err := blob.Row(rowIdx)
			require.NoError(b, err)
			rows = append(rows, &types.BlobRow{
				Index: uint32(rowIdx),
				Data:  rowProof.Row,
				Proof: rowProof.RowProof.RowProof,
			})
		}

		shards[v] = shardEntry{
			promise: makeTestPaymentPromise(uint64(v), blob.ID()),
			shard: &types.BlobShard{
				Rows: rows,
				Root: make([]byte, 32),
			},
			pruneAt: baseTime.Add(time.Duration(v) * time.Minute),
		}
	}

	// Calculate sizes for reporting
	totalRowsWritten := 0
	totalBytesWritten := 0
	for _, s := range shards {
		totalRowsWritten += len(s.shard.Rows)
		for _, row := range s.shard.Rows {
			totalBytesWritten += len(row.Data) + len(row.Proof)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		b.StopTimer()
		store := makeBenchStore(b)
		b.StartTimer()

		// Distribute shards across concurrent workers
		shardCh := make(chan shardEntry, len(shards))
		for _, s := range shards {
			shardCh <- s
		}
		close(shardCh)

		errCh := make(chan error, concurrency)
		for range concurrency {
			go func() {
				var lastErr error
				for s := range shardCh {
					if err := store.Put(b.Context(), s.promise, s.shard, s.pruneAt); err != nil {
						lastErr = err
					}
				}
				errCh <- lastErr
			}()
		}

		// Wait for all workers
		for range concurrency {
			if err := <-errCh; err != nil {
				b.Fatal(err)
			}
		}

		b.StopTimer()
		store.Close()
		b.StartTimer()
	}

	b.StopTimer()

	elapsed := b.Elapsed()
	iterations := float64(b.N)
	totalOps := iterations * float64(validators)

	rowsPerSec := (iterations * float64(totalRowsWritten)) / elapsed.Seconds()
	goodputMBps := (iterations * float64(dataSize)) / elapsed.Seconds() / (1 << 20)
	throughputMBps := (iterations * float64(totalBytesWritten)) / elapsed.Seconds() / (1 << 20)

	b.ReportMetric(rowsPerSec, "rows/sec")
	b.ReportMetric(goodputMBps, "goodput-MiB/s")
	b.ReportMetric(throughputMBps, "throughput-MiB/s")
	b.ReportMetric(totalOps/elapsed.Seconds(), "shards/sec")

	fmt.Printf("\n=== Store Write Performance (Concurrent) ===\n")
	fmt.Printf("Data size: %d MiB, Row size: %d bytes, Concurrency: %d\n", dataSize/(1<<20), rowSize, concurrency)
	fmt.Printf("Rows written: %d, Total bytes: %d MiB\n", totalRowsWritten, totalBytesWritten/(1<<20))
	fmt.Printf("Rows/sec: %.0f\n", rowsPerSec)
	fmt.Printf("Goodput: %.2f MiB/s (raw data)\n", goodputMBps)
	fmt.Printf("Throughput: %.2f MiB/s (with encoding)\n", throughputMBps)
	fmt.Printf("Shards/sec: %.0f\n", totalOps/elapsed.Seconds())
}

// BenchmarkStoreReadRows measures sequential row read performance across blob sizes.
// Reports rows/sec, goodput (raw data throughput), and total throughput.
//
// Run with: go test -bench=BenchmarkStoreReadRows$ -benchmem -benchtime=3x -run=^$
//
// CPU: AMD Ryzen 9 7940HS w/ Radeon 780M Graphics
// Results (100 validators, 148 rows/shard, Badger with ValueThreshold=1KB):
//
//	Blob Size    Row Size    Rows/sec    Goodput      Throughput    Memory/op
//	1 KiB        64 B        989K        0.07 MiB/s   74 MiB/s      37 MB
//	16 KiB       64 B        1.08M       1.1 MiB/s    80 MiB/s      37 MB
//	128 KiB      64 B        1.63M       14 MiB/s     121 MiB/s     37 MB
//	1 MiB        320 B       1.31M       88 MiB/s     416 MiB/s     49 MB
//	16 MiB       4160 B      341K        369 MiB/s    1359 MiB/s    229 MB
//	128 MiB      32768 B     78K         672 MiB/s    2430 MiB/s    1489 MB
//
// Key observations:
//   - Read performance scales better than write for large blobs
//   - Peak goodput at 128 MiB: ~672 MiB/s (3.1x faster than writes!)
//   - Memory usage dominated by unmarshaling all rows into memory
func BenchmarkStoreReadRows(b *testing.B) {
	const validators = 100
	params := fibre.DefaultProtocolParams

	blobSizes := []struct {
		name string
		size int
	}{
		{"1_KiB", 1 << 10},
		{"16_KiB", 16 << 10},
		{"128_KiB", 128 << 10},
		{"1_MiB", 1 << 20},
		{"16_MiB", 16 << 20},
		{"128_MiB", 128 << 20},
	}

	for _, bs := range blobSizes {
		b.Run(bs.name, func(b *testing.B) {
			benchmarkStoreReadRows(b, params, validators, bs.size)
		})
	}
}

// BenchmarkStoreReadRowsConcurrent measures concurrent row read performance.
// Tests different blob sizes with varying concurrency levels.
//
// Run with: go test -bench=BenchmarkStoreReadRowsConcurrent -benchmem -benchtime=3x -run=^$
//
// CPU: AMD Ryzen 9 7940HS w/ Radeon 780M Graphics
// Results (100 validators, 148 rows/shard, Badger with ValueThreshold=1KB):
//
//	Blob Size    Concurrency    Goodput      Throughput    Memory/op
//	1 MiB        2              94 MiB/s     444 MiB/s     98 MB
//	1 MiB        4              127 MiB/s    597 MiB/s     196 MB
//	1 MiB        8              178 MiB/s    839 MiB/s     392 MB
//	1 MiB        16             190 MiB/s    897 MiB/s     784 MB
//	16 MiB       2              626 MiB/s    2306 MiB/s    459 MB
//	16 MiB       4              632 MiB/s    2326 MiB/s    917 MB
//	16 MiB       8              567 MiB/s    2088 MiB/s    1834 MB
//	16 MiB       16             618 MiB/s    2275 MiB/s    3669 MB
//	128 MiB      2              643 MiB/s    2324 MiB/s    2979 MB
//	128 MiB      4              474 MiB/s    1714 MiB/s    5957 MB
//	128 MiB      8              401 MiB/s    1450 MiB/s    11915 MB
//	128 MiB      16             123 MiB/s    445 MiB/s     23829 MB
//
// Key observations:
//   - Peak goodput at 128 MiB with 2 concurrent readers: ~643 MiB/s
//   - Peak goodput at 16 MiB with 2-4 concurrent readers: ~632 MiB/s
//   - Performance degrades at high concurrency for large blobs (memory pressure)
//   - Small blobs benefit from concurrency; large blobs are memory-bound
func BenchmarkStoreReadRowsConcurrent(b *testing.B) {
	const validators = 100
	params := fibre.DefaultProtocolParams

	blobSizes := []struct {
		name string
		size int
	}{
		{"1_MiB", 1 << 20},
		{"16_MiB", 16 << 20},
		{"128_MiB", 128 << 20},
	}

	concurrencyLevels := []int{2, 4, 8, 16}

	for _, bs := range blobSizes {
		for _, conc := range concurrencyLevels {
			name := fmt.Sprintf("%s/conc_%d", bs.name, conc)
			b.Run(name, func(b *testing.B) {
				benchmarkStoreReadRowsConcurrent(b, params, validators, bs.size, conc)
			})
		}
	}
}

func benchmarkStoreReadRows(b *testing.B, params fibre.ProtocolParams, validators int, blobSize int) {
	cfg := fibre.NewBlobConfigFromParams(0, params)

	dataSize := min(blobSize, cfg.MaxDataSize)

	data := make([]byte, dataSize)
	_, err := rand.Read(data)
	require.NoError(b, err)

	blob, err := fibre.NewBlob(data, cfg)
	require.NoError(b, err)

	rowsPerShard := params.MinRowsPerValidator()
	totalRows := params.TotalRows()
	rowSize := blob.RowSize()
	blobID := blob.ID()
	commitment := blobID.Commitment()

	b.Logf("DataSize: %d KiB, RowSize: %d bytes, RowsPerShard: %d, TotalDistributed: %d",
		dataSize/1024, rowSize, rowsPerShard, rowsPerShard*validators)

	// Pre-generate and store all shards
	store := makeBenchStore(b)
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	totalRowsWritten := 0
	totalBytesWritten := 0

	for v := range validators {
		rows := make([]*types.BlobRow, 0, rowsPerShard)
		startIdx := v * rowsPerShard
		for r := 0; r < rowsPerShard && startIdx+r < totalRows; r++ {
			rowIdx := startIdx + r
			rowProof, err := blob.Row(rowIdx)
			require.NoError(b, err)
			rows = append(rows, &types.BlobRow{
				Index: uint32(rowIdx),
				Data:  rowProof.Row,
				Proof: rowProof.RowProof.RowProof,
			})
			totalRowsWritten++
			totalBytesWritten += len(rowProof.Row) + len(rowProof.RowProof.RowProof)
		}

		promise := makeTestPaymentPromise(uint64(v), blobID)
		shard := &types.BlobShard{
			Rows: rows,
			Root: make([]byte, 32),
		}
		err := store.Put(b.Context(), promise, shard, baseTime.Add(time.Duration(v)*time.Minute))
		require.NoError(b, err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		shard, err := store.Get(b.Context(), commitment)
		if err != nil {
			b.Fatal(err)
		}
		if len(shard.Rows) != totalRowsWritten {
			b.Fatalf("expected %d rows, got %d", totalRowsWritten, len(shard.Rows))
		}
	}

	b.StopTimer()
	store.Close()

	elapsed := b.Elapsed()
	iterations := float64(b.N)

	rowsPerSec := (iterations * float64(totalRowsWritten)) / elapsed.Seconds()
	goodputMBps := (iterations * float64(dataSize)) / elapsed.Seconds() / (1 << 20)
	throughputMBps := (iterations * float64(totalBytesWritten)) / elapsed.Seconds() / (1 << 20)

	b.ReportMetric(rowsPerSec, "rows/sec")
	b.ReportMetric(goodputMBps, "goodput-MiB/s")
	b.ReportMetric(throughputMBps, "throughput-MiB/s")

	fmt.Printf("\n=== Store Read Performance ===\n")
	fmt.Printf("Data size: %d MiB, Row size: %d bytes\n", dataSize/(1<<20), rowSize)
	fmt.Printf("Rows read: %d, Total bytes: %d MiB\n", totalRowsWritten, totalBytesWritten/(1<<20))
	fmt.Printf("Rows/sec: %.0f\n", rowsPerSec)
	fmt.Printf("Goodput: %.2f MiB/s (raw data)\n", goodputMBps)
	fmt.Printf("Throughput: %.2f MiB/s (with encoding)\n", throughputMBps)
}

func benchmarkStoreReadRowsConcurrent(b *testing.B, params fibre.ProtocolParams, validators int, blobSize int, concurrency int) {
	cfg := fibre.NewBlobConfigFromParams(0, params)

	dataSize := min(blobSize, cfg.MaxDataSize)

	data := make([]byte, dataSize)
	_, err := rand.Read(data)
	require.NoError(b, err)

	blob, err := fibre.NewBlob(data, cfg)
	require.NoError(b, err)

	rowsPerShard := params.MinRowsPerValidator()
	totalRows := params.TotalRows()
	rowSize := blob.RowSize()
	blobID := blob.ID()
	commitment := blobID.Commitment()

	b.Logf("DataSize: %d KiB, RowSize: %d bytes, RowsPerShard: %d, Concurrency: %d",
		dataSize/1024, rowSize, rowsPerShard, concurrency)

	// Pre-generate and store all shards
	store := makeBenchStore(b)
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	totalRowsWritten := 0
	totalBytesWritten := 0

	for v := range validators {
		rows := make([]*types.BlobRow, 0, rowsPerShard)
		startIdx := v * rowsPerShard
		for r := 0; r < rowsPerShard && startIdx+r < totalRows; r++ {
			rowIdx := startIdx + r
			rowProof, err := blob.Row(rowIdx)
			require.NoError(b, err)
			rows = append(rows, &types.BlobRow{
				Index: uint32(rowIdx),
				Data:  rowProof.Row,
				Proof: rowProof.RowProof.RowProof,
			})
			totalRowsWritten++
			totalBytesWritten += len(rowProof.Row) + len(rowProof.RowProof.RowProof)
		}

		promise := makeTestPaymentPromise(uint64(v), blobID)
		shard := &types.BlobShard{
			Rows: rows,
			Root: make([]byte, 32),
		}
		err := store.Put(b.Context(), promise, shard, baseTime.Add(time.Duration(v)*time.Minute))
		require.NoError(b, err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		errCh := make(chan error, concurrency)
		for range concurrency {
			go func() {
				shard, err := store.Get(b.Context(), commitment)
				if err == nil && len(shard.Rows) != totalRowsWritten {
					err = fmt.Errorf("expected %d rows, got %d", totalRowsWritten, len(shard.Rows))
				}
				errCh <- err
			}()
		}

		for range concurrency {
			if err := <-errCh; err != nil {
				b.Fatal(err)
			}
		}
	}

	b.StopTimer()
	store.Close()

	elapsed := b.Elapsed()
	iterations := float64(b.N)
	totalOps := iterations * float64(concurrency)

	rowsPerSec := (totalOps * float64(totalRowsWritten)) / elapsed.Seconds()
	goodputMBps := (totalOps * float64(dataSize)) / elapsed.Seconds() / (1 << 20)
	throughputMBps := (totalOps * float64(totalBytesWritten)) / elapsed.Seconds() / (1 << 20)

	b.ReportMetric(rowsPerSec, "rows/sec")
	b.ReportMetric(goodputMBps, "goodput-MiB/s")
	b.ReportMetric(throughputMBps, "throughput-MiB/s")
	b.ReportMetric(totalOps/elapsed.Seconds(), "reads/sec")

	fmt.Printf("\n=== Store Read Performance (Concurrent) ===\n")
	fmt.Printf("Data size: %d MiB, Row size: %d bytes, Concurrency: %d\n", dataSize/(1<<20), rowSize, concurrency)
	fmt.Printf("Rows read: %d, Total bytes: %d MiB\n", totalRowsWritten, totalBytesWritten/(1<<20))
	fmt.Printf("Rows/sec: %.0f\n", rowsPerSec)
	fmt.Printf("Goodput: %.2f MiB/s (raw data)\n", goodputMBps)
	fmt.Printf("Throughput: %.2f MiB/s (with encoding)\n", throughputMBps)
	fmt.Printf("Reads/sec: %.0f\n", totalOps/elapsed.Seconds())
}

// BenchmarkStoreBackendComparison compares Badger vs Pebble backends for write and read operations.
// Tests sequential writes and reads at key blob sizes.
//
// Run with: go test -bench=BenchmarkStoreBackendComparison -benchmem -benchtime=3x -run=^$
//
// CPU: AMD Ryzen 9 7940HS w/ Radeon 780M Graphics
// Results (100 validators, 148 rows/shard):
// - Badger: tuned with ValueThreshold=1KB
// - Pebble: tuned with MemTableSize=16MB, ValueSeparation.MinimumSize=4KB
//
//	Backend    Operation    Blob Size    Goodput      Memory/op    Allocs/op
//	Badger     write        1 MiB        46 MiB/s     25 MB        11K
//	Badger     write        16 MiB       205 MiB/s    138 MB       11K
//	Badger     write        128 MiB      224 MiB/s    985 MB       11K
//	Badger     read         1 MiB        100 MiB/s    49 MB        313K
//	Badger     read         16 MiB       490 MiB/s    229 MB       313K
//	Badger     read         128 MiB      601 MiB/s    1489 MB      313K
//	Pebble     write        1 MiB        35 MiB/s     15 MB        3K
//	Pebble     write        16 MiB       171 MiB/s    86 MB        7K
//	Pebble     write        128 MiB      191 MiB/s    1705 MB      29K
//	Pebble     read         1 MiB        112 MiB/s    37 MB        312K
//	Pebble     read         16 MiB       518 MiB/s    162 MB       314K
//	Pebble     read         128 MiB      620 MiB/s    997 MB       314K
//
// Key observations:
//   - Badger: faster writes across all sizes (+31% at 1 MiB, +20% at 16 MiB, +17% at 128 MiB)
//   - Pebble: faster reads for small/medium blobs (+12% at 1 MiB, +6% at 16 MiB)
//   - Pebble: slightly faster reads for large blobs (+3% at 128 MiB)
//   - Pebble uses less memory for small writes but more for large writes
//   - Pebble has fewer allocations for writes
func BenchmarkStoreBackendComparison(b *testing.B) {
	const validators = 100
	params := fibre.DefaultProtocolParams

	blobSizes := []struct {
		name string
		size int
	}{
		{"1_MiB", 1 << 20},
		{"16_MiB", 16 << 20},
		{"128_MiB", 128 << 20},
	}

	for _, bs := range blobSizes {
		b.Run(fmt.Sprintf("pebble/write/%s", bs.name), func(b *testing.B) {
			benchmarkStoreWriteRows(b, params, validators, bs.size)
		})
	}
	for _, bs := range blobSizes {
		b.Run(fmt.Sprintf("pebble/read/%s", bs.name), func(b *testing.B) {
			benchmarkStoreReadRows(b, params, validators, bs.size)
		})
	}
}
