package field_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/field"
)

func TestGF16FromLeopard(t *testing.T) {
	slab := make([]byte, 2*field.LeopardChunkSize)
	for r := range 2 * field.LeopardSymbolsPerChunk {
		chunk := r / field.LeopardSymbolsPerChunk
		slot := r % field.LeopardSymbolsPerChunk
		off := chunk * field.LeopardChunkSize
		slab[off+slot] = byte(r * 2)
		slab[off+field.LeopardSymbolsPerChunk+slot] = byte(r*2 + 1)
	}

	for r := range 2 * field.LeopardSymbolsPerChunk {
		want := uint16((r*2+1)<<8 | (r * 2))
		if got := field.GF16FromLeopard(slab, r); got != want {
			t.Fatalf("symbol %d: got %04x, want %04x", r, got, want)
		}
	}
}

func TestLeopardGF128RoundTrip(t *testing.T) {
	value := field.GF128{0x0102, 0x0304, 0x0506, 0x0708, 0x090a, 0x0b0c, 0x0d0e, 0x0f10}
	chunk := make([]byte, field.LeopardChunkSize)
	for i := range chunk {
		chunk[i] = 0xff
	}

	field.GF128ToLeopard(value, chunk)
	if got := field.GF128FromLeopard(chunk); !field.Equal128(got, value) {
		t.Fatalf("round trip mismatch: got %v, want %v", got, value)
	}
	for i := field.GF128Width; i < field.LeopardSymbolsPerChunk; i++ {
		if chunk[i] != 0 || chunk[field.LeopardSymbolsPerChunk+i] != 0 {
			t.Fatalf("padding slot %d was not cleared", i)
		}
	}
}
