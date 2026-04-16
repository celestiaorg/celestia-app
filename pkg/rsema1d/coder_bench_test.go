package rsema1d

import (
	"math/rand/v2"
	"testing"
)

func BenchmarkCoderEncode(b *testing.B) {
	sizes := []struct {
		name    string
		k, n    int
		rowSize int
	}{
		{"4x4x64", 4, 4, 64},
		{"64x64x512", 64, 64, 512},
		{"1024x1024x1024", 1024, 1024, 1024},
		{"4096x12288x8192", 4096, 12288, 8192},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			coder, err := NewCoder(&Config{K: sz.k, N: sz.n, WorkerCount: 1})
			if err != nil {
				b.Fatal(err)
			}

			rows := make([][]byte, sz.k+sz.n)
			for i := range sz.k {
				rows[i] = make([]byte, sz.rowSize)
				for j := range sz.rowSize {
					rows[i][j] = byte(rand.IntN(256))
				}
			}
			for i := sz.k; i < sz.k+sz.n; i++ {
				rows[i] = make([]byte, sz.rowSize)
			}

			b.SetBytes(int64(sz.k * sz.rowSize))
			b.ResetTimer()
			for b.Loop() {
				for i := sz.k; i < sz.k+sz.n; i++ {
					clear(rows[i])
				}
				if _, err := coder.Encode(rows); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCoderReconstruct(b *testing.B) {
	sizes := []struct {
		name    string
		k, n    int
		rowSize int
	}{
		{"4x4x64", 4, 4, 64},
		{"64x64x512", 64, 64, 512},
		{"1024x1024x1024", 1024, 1024, 1024},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			config := &Config{K: sz.k, N: sz.n, RowSize: sz.rowSize, WorkerCount: 1}
			data := make([][]byte, sz.k)
			for i := range sz.k {
				data[i] = make([]byte, sz.rowSize)
				for j := range sz.rowSize {
					data[i][j] = byte(rand.IntN(256))
				}
			}

			extData, _, _, err := Encode(data, config)
			if err != nil {
				b.Fatal(err)
			}

			// mixed indices: half original, half parity
			indices := make([]int, sz.k)
			halfK := sz.k / 2
			for i := range halfK {
				indices[i] = i
			}
			for i := range sz.k - halfK {
				indices[halfK+i] = sz.k + i
			}

			rows := make([][]byte, sz.k)
			for i, idx := range indices {
				rows[i] = extData.rows[idx]
			}

			coder, _ := NewCoder(&Config{K: sz.k, N: sz.n, WorkerCount: 1})
			b.SetBytes(int64(sz.k * sz.rowSize))
			b.ResetTimer()
			for b.Loop() {
				reconRows := make([][]byte, sz.k+sz.n)
				for i, idx := range indices {
					reconRows[idx] = rows[i]
				}
				if _, err := coder.Reconstruct(reconRows); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
