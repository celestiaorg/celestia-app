package fibre

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
)

// BenchmarkAssemblyFreed measures the Assembly.Freed hot path. During
// upload each per-validator goroutine calls Blob.Row → Assembly.Freed
// once per assigned row — thousands of calls total, hundreds of
// goroutines concurrent. The lock on Assembly is the bottleneck here.
// The parallel variant is the one that exposes reader-vs-reader contention.
func BenchmarkAssemblyFreed(b *testing.B) {
	codec := &rsema1d.Config{K: 4096, N: 12288, WorkerCount: 1}
	a, err := NewRowAssembler(codec, 32832)
	if err != nil {
		b.Fatal(err)
	}
	// Tiny data — we only care about Freed latency, not encoding cost.
	data := []byte("x")
	_, asm := a.Assemble(data, 64, 0)
	defer asm.Free(nil)

	totalRows := codec.K + codec.N

	b.Run("serial", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		i := 0
		for b.Loop() {
			_ = asm.Freed(i % totalRows)
			i++
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				_ = asm.Freed(i % totalRows)
				i++
			}
		})
	})
}

func BenchmarkRowAssemblerAssembleRelease(b *testing.B) {
	codec := &rsema1d.Config{K: 4096, N: 12288, WorkerCount: 1}
	cases := []struct {
		name    string
		rowSize int
	}{
		{"64", 64},
		{"1024", 1024},
		{"32832", 32832},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			a, err := NewRowAssembler(codec, 32832)
			if err != nil {
				b.Fatal(err)
			}
			dataLen := max(1, tc.rowSize-5)
			data := make([]byte, dataLen)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				rows, rel := a.Assemble(data, tc.rowSize, 5)
				rows[0][0]++
				rel.Free(nil)
			}
		})
	}
}

func BenchmarkRowAssemblerEncode(b *testing.B) {
	cases := []struct {
		name       string
		k, n       int
		rowSize    int
		maxRowSize int
	}{
		{"32x32x64", 32, 32, 64, 256},
		{"256x256x1024", 256, 256, 1024, 4096},
		{"256x256x4096", 256, 256, 4096, 4096},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			codec := &rsema1d.Config{K: tc.k, N: tc.n, WorkerCount: 1}
			coder, err := rsema1d.NewCoder(codec)
			if err != nil {
				b.Fatal(err)
			}
			a, err := NewRowAssembler(codec, tc.maxRowSize)
			if err != nil {
				b.Fatal(err)
			}

			data := make([]byte, max(1, tc.k*tc.rowSize-5))
			for i := range data {
				data[i] = byte(i)
			}

			// warm up
			rows, rel := a.Assemble(data, tc.rowSize, 5)
			if _, err := coder.Encode(rows); err != nil {
				b.Fatal(err)
			}
			rel.Free(nil)

			b.ReportAllocs()
			b.SetBytes(int64(tc.k * tc.rowSize))
			b.ResetTimer()
			for b.Loop() {
				rows, rel := a.Assemble(data, tc.rowSize, 5)
				if _, err := coder.Encode(rows); err != nil {
					b.Fatal(err)
				}
				rel.Free(nil)
			}
		})
	}
}
