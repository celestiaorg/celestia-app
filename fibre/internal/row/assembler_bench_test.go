package row

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
)

// BenchmarkAssemblyReleased measures the hot read path. During upload each
// per-validator goroutine calls Blob.Row → Assembly.Released once per row.
// The parallel variant exposes reader-vs-reader RWMutex contention.
func BenchmarkAssemblyReleased(b *testing.B) {
	const k, n = 4096, 12288
	const maxRow = 32832
	a, err := NewAssembler(k, n, maxRow)
	if err != nil {
		b.Fatal(err)
	}
	data := []byte("x")
	asm := a.Assemble(data, 64, 0)
	defer asm.Free()

	b.Run("serial", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_ = asm.Released()
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = asm.Released()
			}
		})
	})
}

func BenchmarkAssemblerAssembleRelease(b *testing.B) {
	const k, n = 4096, 12288
	const maxRow = 32832
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
			a, err := NewAssembler(k, n, maxRow)
			if err != nil {
				b.Fatal(err)
			}
			dataLen := max(1, tc.rowSize-5)
			data := make([]byte, dataLen)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				asm := a.Assemble(data, tc.rowSize, 5)
				rows := asm.Rows()
				rows[0][0]++
				asm.Free()
			}
		})
	}
}

func BenchmarkAssemblerEncode(b *testing.B) {
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
			a, err := NewAssembler(tc.k, tc.n, tc.maxRowSize)
			if err != nil {
				b.Fatal(err)
			}

			data := make([]byte, max(1, tc.k*tc.rowSize-5))
			for i := range data {
				data[i] = byte(i)
			}

			// warm up
			asm := a.Assemble(data, tc.rowSize, 5)
			if _, err := coder.Encode(asm.Rows()); err != nil {
				b.Fatal(err)
			}
			asm.Free()

			b.ReportAllocs()
			b.SetBytes(int64(tc.k * tc.rowSize))
			b.ResetTimer()
			for b.Loop() {
				asm := a.Assemble(data, tc.rowSize, 5)
				if _, err := coder.Encode(asm.Rows()); err != nil {
					b.Fatal(err)
				}
				asm.Free()
			}
		})
	}
}
