package field

import "testing"

func TestGF16FromLeopard(t *testing.T) {
	slab := make([]byte, 2*LeopardChunkSize)
	for r := range 2 * LeopardSymbolsPerChunk {
		chunk := r / LeopardSymbolsPerChunk
		slot := r % LeopardSymbolsPerChunk
		off := chunk * LeopardChunkSize
		slab[off+slot] = byte(r * 2)
		slab[off+LeopardSymbolsPerChunk+slot] = byte(r*2 + 1)
	}

	for r := range 2 * LeopardSymbolsPerChunk {
		want := GF16((r*2+1)<<8 | (r * 2))
		if got := GF16FromLeopard(slab, r); got != want {
			t.Fatalf("symbol %d: got %04x, want %04x", r, got, want)
		}
	}
}

func TestLeopardGF128RoundTrip(t *testing.T) {
	value := GF128{0x0102, 0x0304, 0x0506, 0x0708, 0x090a, 0x0b0c, 0x0d0e, 0x0f10}
	chunk := make([]byte, LeopardChunkSize)
	for i := range chunk {
		chunk[i] = 0xff
	}

	GF128ToLeopard(value, chunk)
	if got := GF128FromLeopard(chunk); !Equal128(got, value) {
		t.Fatalf("round trip mismatch: got %v, want %v", got, value)
	}
	for i := GF128Width; i < LeopardSymbolsPerChunk; i++ {
		if chunk[i] != 0 || chunk[LeopardSymbolsPerChunk+i] != 0 {
			t.Fatalf("padding slot %d was not cleared", i)
		}
	}
}
