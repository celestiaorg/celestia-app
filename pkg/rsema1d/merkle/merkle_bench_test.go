package merkle

import (
	"fmt"
	"math/bits"
	"testing"
)

// Benchmark results (AMD Ryzen 9 7940HS):
// BenchmarkNewTree/leaves_64-16            11,633 ns/op      4,472 B/op       9 allocs/op
// BenchmarkNewTree/leaves_256-16           80,481 ns/op     21,527 B/op      47 allocs/op
// BenchmarkNewTree/leaves_1024-16         338,359 ns/op     86,115 B/op      85 allocs/op
// BenchmarkNewTree/leaves_4096-16       1,315,257 ns/op    343,291 B/op     124 allocs/op
func BenchmarkNewTree(b *testing.B) {
	for _, size := range []int{64, 256, 1024, 4096} {
		b.Run(fmt.Sprintf("leaves_%d", size), func(b *testing.B) {
			leaves := makeTestLeaves(size)
			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				_ = NewTree(leaves)
			}
		})
	}
}

// Benchmark results (AMD Ryzen 9 7940HS):
// BenchmarkComputeRootFromProof/depth_4-16      564.3 ns/op    0 B/op    0 allocs/op
// BenchmarkComputeRootFromProof/depth_8-16       1126 ns/op    0 B/op    0 allocs/op
// BenchmarkComputeRootFromProof/depth_12-16      1491 ns/op    0 B/op    0 allocs/op
func BenchmarkComputeRootFromProof(b *testing.B) {
	for _, numLeaves := range []int{16, 256, 4096} {
		b.Run(fmt.Sprintf("depth_%d", bits.Len(uint(numLeaves-1))), func(b *testing.B) {
			leaves := makeTestLeaves(numLeaves)
			tree := NewTree(leaves)

			// Generate proof for middle leaf
			index := numLeaves / 2
			proof, _ := tree.GenerateProof(index)
			leaf := leaves[index]

			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				_, _ = ComputeRootFromProof(leaf, index, proof)
			}
		})
	}
}

// Benchmark results (AMD Ryzen 9 7940HS):
// BenchmarkGenerateProof/depth_4-16      54.38 ns/op     96 B/op    1 allocs/op
// BenchmarkGenerateProof/depth_8-16      75.04 ns/op    192 B/op    1 allocs/op
// BenchmarkGenerateProof/depth_12-16     103.0 ns/op    288 B/op    1 allocs/op
func BenchmarkGenerateProof(b *testing.B) {
	for _, numLeaves := range []int{16, 256, 4096} {
		b.Run(fmt.Sprintf("depth_%d", bits.Len(uint(numLeaves-1))), func(b *testing.B) {
			leaves := makeTestLeaves(numLeaves)
			tree := NewTree(leaves)
			index := numLeaves / 2

			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				_, _ = tree.GenerateProof(index)
			}
		})
	}
}
