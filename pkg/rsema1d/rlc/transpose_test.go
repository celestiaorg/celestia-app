package rlc

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"testing"
)

func TestTransposeChunkReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(17, 29))
	for _, k := range []int{32, 64, 96, 1024} {
		for _, c := range []int{0, 1, 3} {
			t.Run(fmt.Sprintf("rows=%d/chunk=%d", k, c), func(t *testing.T) {
				rows := make([][]byte, k)
				for r := range rows {
					// Deliberately unaligned input and output.
					rows[r] = make([]byte, 4*chunkSize+1)[1:]
					for i := range rows[r] {
						rows[r][i] = byte(rng.Uint32())
					}
				}
				buf := bytes.Repeat([]byte{0xa5}, k*chunkSize+2)
				got := buf[1 : len(buf)-1]
				want := make([]byte, len(got))
				for j := range symbolsPerChunk {
					for r := range k {
						off := j*2*k + (r/symbolsPerChunk)*chunkSize + r%symbolsPerChunk
						want[off] = rows[r][c*chunkSize+j]
						want[off+symbolsPerChunk] = rows[r][c*chunkSize+symbolsPerChunk+j]
					}
				}
				transposeChunk(got, k, rows, c)
				if !bytes.Equal(got, want) || buf[0] != 0xa5 || buf[len(buf)-1] != 0xa5 {
					t.Fatal("transpose differs from reference or overwrote guard bytes")
				}
			})
		}
	}
}

func TestTransposeTileReference(t *testing.T) {
	var tile [symbolsPerChunk * chunkSize]byte
	rng := rand.New(rand.NewPCG(3, 9))
	for i := range tile {
		tile[i] = byte(rng.Uint32())
	}
	for _, stride := range []int{64, 65, 128, 2048} {
		// Guard every gap as well as the beginning and end of the destination.
		got := bytes.Repeat([]byte{0xa5}, (symbolsPerChunk-1)*stride+chunkSize+2)
		want := bytes.Clone(got)
		transposeTile(got[1:len(got)-1], stride, &tile)
		// Reconstruct the expected scatter independently of transposeTile.
		dst := want[1 : len(want)-1]
		for j := range symbolsPerChunk {
			for r := range symbolsPerChunk {
				dst[j*stride+r] = tile[r*chunkSize+j]
				dst[j*stride+symbolsPerChunk+r] = tile[r*chunkSize+symbolsPerChunk+j]
			}
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("stride %d differs from reference scatter", stride)
		}
	}
}

func BenchmarkTransposeTile(b *testing.B) {
	for _, stride := range []int{64, 2048, 8192} {
		b.Run(fmt.Sprintf("stride=%d", stride), func(b *testing.B) {
			var tile [symbolsPerChunk * chunkSize]byte
			dst := make([]byte, (symbolsPerChunk-1)*stride+chunkSize)
			b.SetBytes(int64(len(tile)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				transposeTile(dst, stride, &tile)
			}
		})
	}
}
