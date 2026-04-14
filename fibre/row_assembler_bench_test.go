package fibre

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
)

func BenchmarkRowAssemblerAssembleRelease(b *testing.B) {
	codecCfg := &rsema1d.Config{K: 4096, N: 12288, WorkerCount: 1}
	cases := []struct {
		name      string
		rowSize   int
		chunkSize int
	}{
		{"64/chunk_512KiB", 64, 1 << 19},
		{"1024/chunk_512KiB", 1024, 1 << 19},
		{"32832/chunk_512KiB", 32832, 1 << 19},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			allocCfg := DefaultRowAssemblerConfig()
			allocCfg.PackedChunkSize = tc.chunkSize
			alloc, err := NewRowAssembler(codecCfg, allocCfg)
			if err != nil {
				b.Fatal(err)
			}
			dataLen := max(1, tc.rowSize-5)
			data := make([]byte, dataLen)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				rows, release := alloc.Assemble(data, tc.rowSize, 5)
				rows[0][0]++
				release()
			}
		})
	}
}

func BenchmarkRowAssemblerEncode(b *testing.B) {
	cases := []struct {
		name     string
		k, n     int
		rowSize  int
		allocCfg *RowAssemblerConfig
	}{
		{
			"32x32x64", 32, 32, 64,
			&RowAssemblerConfig{MaxRowSize: 256, PackedChunkSize: 1 << 19, MaxRowPoolCap: 6},
		},
		{
			"256x256x1024", 256, 256, 1024,
			&RowAssemblerConfig{MaxRowSize: 4096, PackedChunkSize: 1 << 19, MaxRowPoolCap: 6},
		},
		{
			"256x256x4096", 256, 256, 4096,
			&RowAssemblerConfig{MaxRowSize: 4096, PackedChunkSize: 1 << 19, MaxRowPoolCap: 6},
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			codecCfg := &rsema1d.Config{K: tc.k, N: tc.n, WorkerCount: 1}
			coder, err := rsema1d.NewCoder(codecCfg)
			if err != nil {
				b.Fatal(err)
			}
			alloc, err := NewRowAssembler(codecCfg, tc.allocCfg)
			if err != nil {
				b.Fatal(err)
			}

			data := make([]byte, max(1, tc.k*tc.rowSize-5))
			for i := range data {
				data[i] = byte(i)
			}

			// warm up
			rows, release := alloc.Assemble(data, tc.rowSize, 5)
			if _, err := coder.Encode(rows); err != nil {
				b.Fatal(err)
			}
			release()

			b.ReportAllocs()
			b.SetBytes(int64(tc.k * tc.rowSize))
			b.ResetTimer()
			for b.Loop() {
				rows, release := alloc.Assemble(data, tc.rowSize, 5)
				if _, err := coder.Encode(rows); err != nil {
					b.Fatal(err)
				}
				release()
			}
		})
	}
}
