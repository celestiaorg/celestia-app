package merkle

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math/rand/v2"
	"testing"
)

func wantLeaf(data []byte) []byte {
	h := sha256.Sum256(append([]byte{0}, data...))
	return h[:]
}

func randBytes(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Uint32())
	}
	return b
}

func TestHashLeaf2(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	var lengths []int
	for n := range 301 {
		lengths = append(lengths, n)
	}
	lengths = append(lengths, 32768, 32769, 65553)
	for _, n := range lengths {
		a, b := randBytes(rng, n), randBytes(rng, n)
		da, db := make([]byte, NodeSize), make([]byte, NodeSize)
		hashLeaf2(a, b, da, db)
		if !bytes.Equal(da, wantLeaf(a)) || !bytes.Equal(db, wantLeaf(b)) {
			t.Fatalf("len %d: digest mismatch", n)
		}
	}
}

func TestHashLeaf2Unaligned(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for _, n := range []int{63, 64, 100, 1000, 32768} {
		for off := range 8 {
			backing := randBytes(rng, 2*n+16)
			a, b := backing[off:off+n], backing[off+n+3:off+2*n+3]
			da, db := make([]byte, NodeSize), make([]byte, NodeSize)
			hashLeaf2(a, b, da, db)
			if !bytes.Equal(da, wantLeaf(a)) || !bytes.Equal(db, wantLeaf(b)) {
				t.Fatalf("len %d off %d: digest mismatch", n, off)
			}
		}
	}
}

func TestHashLeaf2UnequalLengths(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	for _, pair := range [][2]int{{0, 1}, {63, 64}, {100, 200}, {32768, 32769}} {
		a, b := randBytes(rng, pair[0]), randBytes(rng, pair[1])
		da, db := make([]byte, NodeSize), make([]byte, NodeSize)
		hashLeaf2(a, b, da, db)
		if !bytes.Equal(da, wantLeaf(a)) || !bytes.Equal(db, wantLeaf(b)) {
			t.Fatalf("lens %v: digest mismatch", pair)
		}
	}
}

func TestHashLeafPairsMatchesHashLeaf(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	for _, n := range []int{1, 2, 3, 4, 5, 8, 16, 1024} {
		for _, rowSize := range []int{0, 1, 62, 63, 64, 65, 512, 32768} {
			for _, workers := range []int{1, 4} {
				leaves := make([][]byte, n)
				for i := range leaves {
					leaves[i] = randBytes(rng, rowSize)
				}
				got := make([]byte, n*NodeSize)
				hashLeafPairs(n, workers, leaves, func(i int) []byte { return got[i*NodeSize : (i+1)*NodeSize] })
				for i := range leaves {
					if !bytes.Equal(got[i*NodeSize:(i+1)*NodeSize], wantLeaf(leaves[i])) {
						t.Fatalf("n=%d rowSize=%d workers=%d leaf %d: mismatch", n, rowSize, workers, i)
					}
				}
			}
		}
	}
}

// NewTreeInto (paired leaf hashing) must produce the same tree as
// NewTreeFuncInto (one leaf at a time).
func TestNewTreeIntoMatchesFuncInto(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 10))
	for _, n := range []int{1, 2, 4, 8, 16, 1024} {
		for _, rowSize := range []int{1, 63, 64, 100, 4096} {
			leaves := make([][]byte, n)
			for i := range leaves {
				leaves[i] = randBytes(rng, rowSize)
			}
			paired := NewTreeInto(make([]byte, TreeBufferSize(n)), leaves, 4)
			single := NewTreeFuncInto(make([]byte, TreeBufferSize(n)), 4, func(i int, _ []byte) []byte { return leaves[i] })
			if !bytes.Equal(paired.nodes, single.nodes) {
				t.Fatalf("n=%d rowSize=%d: trees differ", n, rowSize)
			}
		}
	}
}

func FuzzHashLeaf2(f *testing.F) {
	f.Add([]byte{}, []byte{})
	f.Add(bytes.Repeat([]byte{1}, 63), bytes.Repeat([]byte{2}, 63))
	f.Add(bytes.Repeat([]byte{1}, 200), bytes.Repeat([]byte{2}, 200))
	f.Fuzz(func(t *testing.T, a, b []byte) {
		da, db := make([]byte, NodeSize), make([]byte, NodeSize)
		hashLeaf2(a, b, da, db)
		if !bytes.Equal(da, wantLeaf(a)) || !bytes.Equal(db, wantLeaf(b)) {
			t.Fatalf("lens %d/%d: digest mismatch", len(a), len(b))
		}
		// equal-length variant exercises the interleaved path
		c := make([]byte, len(a))
		for i := range c {
			c[i] = ^a[i]
		}
		hashLeaf2(a, c, da, db)
		if !bytes.Equal(da, wantLeaf(a)) || !bytes.Equal(db, wantLeaf(c)) {
			t.Fatalf("len %d: equal-length digest mismatch", len(a))
		}
	})
}

func BenchmarkHashLeaf(b *testing.B) {
	const size = 32768
	x, y := make([]byte, size), make([]byte, size)
	dx, dy := make([]byte, NodeSize), make([]byte, NodeSize)
	b.Run("single", func(b *testing.B) {
		b.SetBytes(2 * size)
		for b.Loop() {
			hashLeaf(x, dx)
			hashLeaf(y, dy)
		}
	})
	b.Run("x2", func(b *testing.B) {
		b.SetBytes(2 * size)
		for b.Loop() {
			hashLeaf2(x, y, dx, dy)
		}
	})
}

// BenchmarkNewTreeIntoLarge is the encoder's row-tree shape. FuncInto hashes
// leaves one at a time and serves as the pre-hashLeaf2 baseline.
func BenchmarkNewTreeIntoLarge(b *testing.B) {
	const n, size = 16384, 32768
	leaves := make([][]byte, n)
	backing := make([]byte, n*size)
	for i := range leaves {
		leaves[i] = backing[i*size : (i+1)*size]
	}
	buf := make([]byte, TreeBufferSize(n))
	for _, workers := range []int{1, 16} {
		b.Run(fmt.Sprintf("FuncInto/workers_%d", workers), func(b *testing.B) {
			b.SetBytes(n * size)
			for b.Loop() {
				NewTreeFuncInto(buf, workers, func(i int, _ []byte) []byte { return leaves[i] })
			}
		})
		b.Run(fmt.Sprintf("Into/workers_%d", workers), func(b *testing.B) {
			b.SetBytes(n * size)
			for b.Loop() {
				NewTreeInto(buf, leaves, workers)
			}
		})
	}
}
