package merkle_test

import (
	"bytes"
	"runtime"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/merkle"
	cmtmerkle "github.com/cometbft/cometbft/crypto/merkle"
)

// benchCase is one (leaf count, leaf size) point on the comparison grid.
type benchCase struct {
	name      string
	numLeaves int
	leafSize  int
}

// rowTreeCases model the row tree: many large data-row leaves. Counts stop at
// 16384 because 65536 large leaves would allocate tens of GB across b.N.
var rowTreeCases = []benchCase{
	{"rows_256x2KiB", 256, 2 << 10},
	{"rows_1024x2KiB", 1024, 2 << 10},
	{"rows_4096x2KiB", 4096, 2 << 10},
	{"rows_16384x2KiB", 16384, 2 << 10},
}

// rlcTreeCases model the RLC tree: K tiny 16-byte leaves. Here hashing overhead,
// not leaf bytes, dominates — the regime core guards with its small-leaf
// parallelization threshold (minItemsForSmallLeaves).
var rlcTreeCases = []benchCase{
	{"rlc_1024x16B", 1024, 16},
	{"rlc_4096x16B", 4096, 16},
	{"rlc_16384x16B", 16384, 16},
	{"rlc_65536x16B", 65536, 16},
}

// makeLeaves builds n leaves of the given size with deterministic content.
func makeLeaves(n, size int) [][]byte {
	leaves := make([][]byte, n)
	for i := range leaves {
		leaf := make([]byte, size)
		for j := range leaf {
			leaf[j] = byte((i*31 + j) % 251)
		}
		leaves[i] = leaf
	}
	return leaves
}

// requireSameRoots fails the benchmark unless fibre, core-sequential and
// core-parallel agree, so every timed loop below is measuring equivalent work.
func requireSameRoots(b *testing.B, leaves [][]byte) {
	b.Helper()
	fibre := merkle.NewTree(leaves, runtime.NumCPU()).Root()
	coreSeq := cmtmerkle.HashFromByteSlices(leaves)
	corePar := cmtmerkle.ParallelHashFromByteSlices(leaves)
	if !bytes.Equal(fibre[:], coreSeq) || !bytes.Equal(fibre[:], corePar) {
		b.Fatalf("root mismatch (%d leaves):\n  fibre=%x\n  core/seq=%x\n  core/par=%x",
			len(leaves), fibre, coreSeq, corePar)
	}
}

// BenchmarkCompareRoot times root construction for both implementations across
// the row-tree and RLC-tree workloads. Sub-benchmark names are
// <workload>/<impl>, e.g. rows_4096x2KiB/fibre_par, so `benchstat` can line the
// four implementations up per workload.
func BenchmarkCompareRoot(b *testing.B) {
	workers := runtime.NumCPU()
	for _, tc := range append(append([]benchCase{}, rowTreeCases...), rlcTreeCases...) {
		leaves := makeLeaves(tc.numLeaves, tc.leafSize)
		requireSameRoots(b, leaves)

		b.Run(tc.name+"/fibre_seq", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = merkle.NewTree(leaves, 1)
			}
		})
		b.Run(tc.name+"/fibre_par", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = merkle.NewTree(leaves, workers)
			}
		})
		b.Run(tc.name+"/core_seq", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = cmtmerkle.HashFromByteSlices(leaves)
			}
		})
		b.Run(tc.name+"/core_par", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = cmtmerkle.ParallelHashFromByteSlices(leaves)
			}
		})
	}
}

// BenchmarkCompareProofs times generating an inclusion proof for every leaf
// (the coder/verifier pattern): fibre builds the tree then streams all proofs
// via Tree.Proofs, core uses ParallelProofsFromByteSlices which returns the
// root and every proof in one call.
func BenchmarkCompareProofs(b *testing.B) {
	workers := runtime.NumCPU()
	// Proofs materialize len(leaves) aunt paths, so keep large-leaf counts modest.
	cases := []benchCase{
		{"rows_1024x2KiB", 1024, 2 << 10},
		{"rows_4096x2KiB", 4096, 2 << 10},
		{"rlc_4096x16B", 4096, 16},
		{"rlc_16384x16B", 16384, 16},
	}
	for _, tc := range cases {
		leaves := makeLeaves(tc.numLeaves, tc.leafSize)
		requireSameRoots(b, leaves)
		positions := make([]int, tc.numLeaves)
		for i := range positions {
			positions[i] = i
		}

		b.Run(tc.name+"/fibre_seq", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				tree := merkle.NewTree(leaves, 1)
				_ = tree.Proofs(positions, func(int, [][]byte) {})
			}
		})
		b.Run(tc.name+"/fibre_par", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				tree := merkle.NewTree(leaves, workers)
				_ = tree.Proofs(positions, func(int, [][]byte) {})
			}
		})
		b.Run(tc.name+"/core_par", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = cmtmerkle.ParallelProofsFromByteSlices(leaves)
			}
		})
	}
}
