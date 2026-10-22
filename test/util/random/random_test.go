package random_test

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/test/util/random"
)

func TestBytesConcurrent(t *testing.T) {
	for range 32 {
		t.Run("bytes", func(t *testing.T) {
			t.Parallel()
			for _, size := range []int{0, 1, 7, 1024, 1 << 16} {
				if got := len(random.Bytes(size)); got != size {
					t.Fatalf("got %d bytes, want %d", got, size)
				}
			}
		})
	}
}

func TestBytesRReproducible(t *testing.T) {
	first := rand.New(rand.NewSource(1))
	second := rand.New(rand.NewSource(1))
	for _, size := range []int{0, 1, 7, 1024} {
		if !bytes.Equal(random.BytesR(first, size), random.BytesR(second, size)) {
			t.Fatalf("same seed and calls produced different %d-byte fixtures", size)
		}
	}
}
