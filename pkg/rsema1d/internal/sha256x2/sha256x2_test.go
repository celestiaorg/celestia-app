package sha256x2

import (
	"bytes"
	"crypto/sha256"
	"math/rand/v2"
	"os"
	"runtime"
	"testing"
)

// TestSum compares Sum against crypto/sha256 for every length up to 300 bytes,
// lengths around the 64 KiB block-count boundaries, several alignments, and
// unequal lengths (which take the fallback).
func TestSum(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	lengths := make([]int, 0, 308)
	for n := range 301 {
		lengths = append(lengths, n)
	}
	lengths = append(lengths, 32768, 32769, 65535, 65536, 65537, 70000, 71680)
	for _, n := range lengths {
		for _, off := range []int{0, 1, 3, 7} {
			backing := randBytes(rng, 2*n+off+8)
			a, b := backing[off:off+n], backing[off+n+3:off+2*n+3]
			requireSum(t, byte(n), a, b)
		}
	}
	for _, lens := range [][2]int{{0, 1}, {63, 64}, {100, 200}, {32768, 32769}} {
		requireSum(t, 0, randBytes(rng, lens[0]), randBytes(rng, lens[1]))
	}
}

func FuzzSum(f *testing.F) {
	f.Add(byte(0), []byte{}, []byte{})
	f.Add(byte(0), bytes.Repeat([]byte{1}, 63), bytes.Repeat([]byte{2}, 63))
	f.Add(byte(1), bytes.Repeat([]byte{1}, 200), bytes.Repeat([]byte{2}, 200))
	f.Add(byte(0), bytes.Repeat([]byte{1}, 71680), bytes.Repeat([]byte{2}, 71680))
	f.Fuzz(func(t *testing.T, prefix byte, a, b []byte) {
		requireSum(t, prefix, a, b)
		// an equal-length pair takes the two-lane path
		c := make([]byte, len(a))
		for i := range c {
			c[i] = ^a[i]
		}
		requireSum(t, prefix, a, c)
	})
}

// TestRequireASM logs which path Sum takes. With RSEMA1D_REQUIRE_ASM=1 it fails,
// rather than skips, when an asm-capable arm64 build does not run the two-lane
// path, so a runner-image or CPU change cannot silently drop asm coverage in CI.
func TestRequireASM(t *testing.T) {
	var path string
	switch {
	case !asmBuild:
		path = "portable (two-lane SHA-256 excluded by GOARCH or build tags)"
	case enabled:
		path = "arm64 two-lane SHA-256"
	default:
		path = "portable (arm64 CPU lacks SHA2)"
	}
	t.Logf("SHA-256 x2 path: %s (GOARCH=%s)", path, runtime.GOARCH)

	if os.Getenv("RSEMA1D_REQUIRE_ASM") != "1" {
		return
	}
	switch {
	case runtime.GOARCH != "arm64":
		t.Logf("RSEMA1D_REQUIRE_ASM=1 not enforced on GOARCH=%s", runtime.GOARCH)
	case !asmBuild:
		t.Logf("RSEMA1D_REQUIRE_ASM=1 not enforced: two-lane SHA-256 excluded by build tags (-race/noasm)")
	case !enabled:
		t.Fatal("RSEMA1D_REQUIRE_ASM=1 but the two-lane SHA-256 path is inactive on this arm64 CPU (missing SHA2?)")
	}
}

func BenchmarkSum(b *testing.B) {
	const size = 32768
	x, y := make([]byte, size), make([]byte, size)
	dx, dy := make([]byte, sha256.Size), make([]byte, sha256.Size)
	b.Run("single", func(b *testing.B) {
		b.SetBytes(2 * size)
		for b.Loop() {
			sum(0, x, dx)
			sum(0, y, dy)
		}
	})
	b.Run("x2", func(b *testing.B) {
		b.SetBytes(2 * size)
		for b.Loop() {
			Sum(0, x, y, dx, dy)
		}
	})
}

// requireSum fails unless Sum matches crypto/sha256 for both inputs.
func requireSum(t *testing.T, prefix byte, a, b []byte) {
	t.Helper()
	da, db := make([]byte, sha256.Size), make([]byte, sha256.Size)
	Sum(prefix, a, b, da, db)
	for _, c := range []struct{ data, got []byte }{{a, da}, {b, db}} {
		want := sha256.Sum256(append([]byte{prefix}, c.data...))
		if !bytes.Equal(c.got, want[:]) {
			t.Fatalf("prefix %d, lengths %d/%d: digest mismatch", prefix, len(a), len(b))
		}
	}
}

func randBytes(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Uint32())
	}
	return b
}
