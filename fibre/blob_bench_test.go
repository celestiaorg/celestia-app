package fibre_test

import (
	"bufio"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/fibre"
)

// BenchmarkNewBlob encodes max-size blobs through the default config, serially
// and from 16 goroutines. On Linux it also reports the peak RSS of the run.
//
// Run with: go test -run=^$ -bench=BenchmarkNewBlob -benchtime=20x ./fibre
func BenchmarkNewBlob(b *testing.B) {
	cfg := fibre.DefaultBlobConfigV0()
	data := benchBlobData(cfg.MaxDataSize)
	for _, workers := range []int{1, 16} {
		b.Run("128MiB/workers="+strconv.Itoa(workers), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			resetPeakRSS()
			b.ResetTimer()
			runBlobs(b, workers, b.N, func(int) int { return len(data) }, data, cfg)
			b.StopTimer()
			reportRSS(b)
		})
	}
}

// BenchmarkNewBlobMixedSizes encodes blobs with sizes drawn uniformly from
// 32-128 MiB from 16 goroutines, the shape that left one idle slab per
// distinct size in a per-size pool. Reports peak RSS and the RSS left
// 5 s after the run.
func BenchmarkNewBlobMixedSizes(b *testing.B) {
	cfg := fibre.DefaultBlobConfigV0()
	data := benchBlobData(cfg.MaxDataSize)
	rng := rand.New(rand.NewPCG(1, 2))
	sizes := make([]int, b.N)
	total := 0
	for i := range sizes {
		sizes[i] = 32<<20 + rng.IntN(cfg.MaxDataSize-32<<20+1)
		total += sizes[i]
	}
	b.SetBytes(int64(total / b.N))
	resetPeakRSS()
	b.ResetTimer()
	runBlobs(b, 16, b.N, func(i int) int { return sizes[i] }, data, cfg)
	b.StopTimer()
	reportRSS(b)
	time.Sleep(5 * time.Second)
	reportMetricRSS(b, "idle5s-rss-MiB")
}

// runBlobs encodes n blobs, sizes from size(i), using workers goroutines.
func runBlobs(b *testing.B, workers, n int, size func(int) int, data []byte, cfg fibre.BlobConfig) {
	var (
		wg   sync.WaitGroup
		next = make(chan int)
	)
	for range workers {
		wg.Go(func() {
			for i := range next {
				blob, err := fibre.NewBlob(data[:size(i)], cfg)
				if err != nil {
					b.Error(err)
					return
				}
				blob.Free()
			}
		})
	}
	for i := range n {
		next <- i
	}
	close(next)
	wg.Wait()
}

func benchBlobData(n int) []byte {
	data := make([]byte, n)
	rng := rand.NewChaCha8([32]byte{1})
	_, _ = rng.Read(data)
	return data
}

// resetPeakRSS restarts the kernel's peak-RSS counter (Linux only).
func resetPeakRSS() {
	_ = os.WriteFile("/proc/self/clear_refs", []byte("5"), 0)
}

func reportRSS(b *testing.B) {
	if hwm := procStatusMiB("VmHWM:"); hwm > 0 {
		b.ReportMetric(hwm, "peak-rss-MiB")
	}
}

func reportMetricRSS(b *testing.B, name string) {
	if rss := procStatusMiB("VmRSS:"); rss > 0 {
		b.ReportMetric(rss, name)
	}
}

// procStatusMiB returns a /proc/self/status field in MiB, or 0 off Linux.
func procStatusMiB(field string) float64 {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), field); ok {
			kb, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 64)
			return kb / 1024
		}
	}
	return 0
}
