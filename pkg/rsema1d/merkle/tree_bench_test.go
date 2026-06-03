package merkle_test

import (
	"fmt"
	"math/bits"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/merkle"
)

// Benchmark results (AMD Ryzen AI 9 HX 370):
// BenchmarkNewTree/leaves_64-24       12,923 ns/op      4,200 B/op   5 allocs/op
// BenchmarkNewTree/leaves_256-24      47,609 ns/op     16,488 B/op   5 allocs/op
// BenchmarkNewTree/leaves_1024-24    207,834 ns/op     65,640 B/op   5 allocs/op
// BenchmarkNewTree/leaves_4096-24    779,315 ns/op    262,248 B/op   5 allocs/op
func BenchmarkNewTree(b *testing.B) {
	for _, size := range []int{64, 256, 1024, 4096} {
		b.Run(fmt.Sprintf("leaves_%d", size), func(b *testing.B) {
			leaves := makeTestLeaves(size)
			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				_ = merkle.NewTree(leaves, 1)
			}
		})
	}
}

// RootFromFunc reuses caller-owned buffers across iterations, so the whole
// root computation runs without allocating (see TestCallerOwnedStorageDoesNotAllocate).
// Benchmark results (AMD Ryzen AI 9 HX 370):
// BenchmarkRootFromFunc/leaves_64-24        9,428 ns/op    0 B/op    0 allocs/op
// BenchmarkRootFromFunc/leaves_256-24      38,184 ns/op    0 B/op    0 allocs/op
// BenchmarkRootFromFunc/leaves_1024-24    151,078 ns/op    0 B/op    0 allocs/op
// BenchmarkRootFromFunc/leaves_4096-24    594,228 ns/op    0 B/op    0 allocs/op
func BenchmarkRootFromFunc(b *testing.B) {
	for _, numLeaves := range []int{64, 256, 1024, 4096} {
		b.Run(fmt.Sprintf("leaves_%d", numLeaves), func(b *testing.B) {
			leaves := makeTestLeaves(numLeaves)
			buf := make([]byte, numLeaves*merkle.NodeSize)

			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				_ = merkle.RootFromFunc(buf, func(i int, _ []byte) []byte {
					return leaves[i]
				})
			}
		})
	}
}

// NewTreeFuncInto into a reused buffer + all proofs (coder/verifier hot path):
// allocs are constant (buffer reused); B/op is the proof arena.
// Benchmark results (AMD Ryzen AI 9 HX 370):
// BenchmarkNewTreeFuncIntoProofs/leaves_64-24        13,996 ns/op      9,584 B/op   6 allocs/op
// BenchmarkNewTreeFuncIntoProofs/leaves_256-24       60,942 ns/op     49,264 B/op   6 allocs/op
// BenchmarkNewTreeFuncIntoProofs/leaves_1024-24     259,453 ns/op    245,872 B/op   6 allocs/op
// BenchmarkNewTreeFuncIntoProofs/leaves_4096-24   1,182,653 ns/op  1,179,762 B/op   6 allocs/op
func BenchmarkNewTreeFuncIntoProofs(b *testing.B) {
	for _, numLeaves := range []int{64, 256, 1024, 4096} {
		b.Run(fmt.Sprintf("leaves_%d", numLeaves), func(b *testing.B) {
			leaves := makeTestLeaves(numLeaves)
			buf := make([]byte, merkle.TreeBufferSize(numLeaves))
			positions := make([]int, numLeaves)
			for i := range positions {
				positions[i] = i
			}

			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				tree := merkle.NewTreeFuncInto(buf, 1, func(i int, _ []byte) []byte {
					return leaves[i]
				})
				_ = tree.Proofs(positions, func(int, [][]byte) {})
			}
		})
	}
}

// Benchmark results (AMD Ryzen AI 9 HX 370):
// BenchmarkRootFromProof/depth_4-24      442.0 ns/op    0 B/op    0 allocs/op
// BenchmarkRootFromProof/depth_8-24      845.0 ns/op    0 B/op    0 allocs/op
// BenchmarkRootFromProof/depth_12-24      1237 ns/op    0 B/op    0 allocs/op
func BenchmarkRootFromProof(b *testing.B) {
	for _, numLeaves := range []int{16, 256, 4096} {
		b.Run(fmt.Sprintf("depth_%d", bits.Len(uint(numLeaves-1))), func(b *testing.B) {
			leaves := makeTestLeaves(numLeaves)
			tree := merkle.NewTree(leaves, 1)

			// Generate proof for middle leaf
			index := numLeaves / 2
			proof, _ := tree.Proof(index)
			leaf := leaves[index]

			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				_, _ = merkle.RootFromProof(leaf, index, proof)
			}
		})
	}
}

// RootFromProofs verifies a same-tree batch in parallel; scratch is one root per
// worker chunk, not per input, so B/op stays tiny and flat with the batch size
// (vs len(inputs)*NodeSize before — e.g. 128 KB at 4096 inputs).
// Benchmark results (AMD Ryzen AI 9 HX 370, workers=8):
// BenchmarkRootFromProofs/inputs_256-24       67,467 ns/op    870 B/op   12 allocs/op
// BenchmarkRootFromProofs/inputs_1024-24     239,323 ns/op    865 B/op   12 allocs/op
// BenchmarkRootFromProofs/inputs_4096-24     998,922 ns/op    864 B/op   12 allocs/op
func BenchmarkRootFromProofs(b *testing.B) {
	for _, inputs := range []int{256, 1024, 4096} {
		b.Run(fmt.Sprintf("inputs_%d", inputs), func(b *testing.B) {
			leaves := makeTestLeaves(inputs)
			tree := merkle.NewTree(leaves, 1)
			proofs := make([]merkle.ProofInput, inputs)
			positions := make([]int, inputs)
			for i := range positions {
				positions[i] = i
			}
			_ = tree.Proofs(positions, func(i int, proof [][]byte) {
				proofs[i] = merkle.ProofInput{Leaf: leaves[i], Index: i, Path: proof}
			})

			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				_, _ = merkle.RootFromProofs(proofs, 8)
			}
		})
	}
}

// Benchmark results (AMD Ryzen AI 9 HX 370):
// BenchmarkProof/depth_4-24      45.96 ns/op     96 B/op    1 allocs/op
// BenchmarkProof/depth_8-24      70.90 ns/op    192 B/op    1 allocs/op
// BenchmarkProof/depth_12-24    121.4 ns/op    288 B/op    1 allocs/op
func BenchmarkProof(b *testing.B) {
	for _, numLeaves := range []int{16, 256, 4096} {
		b.Run(fmt.Sprintf("depth_%d", bits.Len(uint(numLeaves-1))), func(b *testing.B) {
			leaves := makeTestLeaves(numLeaves)
			tree := merkle.NewTree(leaves, 1)
			index := numLeaves / 2

			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				_, _ = tree.Proof(index)
			}
		})
	}
}
