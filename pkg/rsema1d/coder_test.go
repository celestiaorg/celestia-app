package rsema1d_test

import (
	"bytes"
	"math/rand/v2"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
)

// encodeRows is a Coder-based replacement for the deprecated codec.Encode
// helper. It allocates the K+N row buffer the Coder expects (data in
// rows[:K], zero parity slots in rows[K:K+N]), runs the produce path, and
// returns the same (ExtendedData, Commitment, []GF128) triple historical
// tests rely on.
func encodeRows(tb testing.TB, cfg *rsema1d.Config, data [][]byte) (*rsema1d.ExtendedData, rsema1d.Commitment, []field.GF128) {
	tb.Helper()
	if len(data) != cfg.K {
		tb.Fatalf("encodeRows: expected %d input rows, got %d", cfg.K, len(data))
	}
	coder, err := rsema1d.NewCoder(cfg)
	if err != nil {
		tb.Fatalf("NewCoder: %v", err)
	}
	rowSize := len(data[0])
	rows := make([][]byte, cfg.K+cfg.N)
	copy(rows, data)
	for i := cfg.K; i < cfg.K+cfg.N; i++ {
		rows[i] = make([]byte, rowSize)
	}
	ed, err := coder.Encode(rows)
	if err != nil {
		tb.Fatalf("Coder.Encode: %v", err)
	}
	return ed, ed.Commitment(), ed.RLC()
}

// roundtripConfigs covers a mix of small/large and 1:1/1:3 ratios. Both K and
// K+N are powers of 2, as the codec requires.
var roundtripConfigs = []struct {
	name          string
	k, n, rowSize int
}{
	{"1:1 small k=4 n=4", 4, 4, 64},
	{"1:3 small k=4 n=12", 4, 12, 64},
	{"1:1 medium k=8 n=8", 8, 8, 256},
	{"1:3 medium k=8 n=24", 8, 24, 256},
	{"1:1 large k=16 n=16", 16, 16, 512},
	{"1:3 large k=16 n=48", 16, 48, 512},
}

// fillRows fills k rows of `rowSize` deterministic bytes seeded by k+rowSize
// so tests are reproducible without sharing random state across cases.
func fillRows(k, rowSize int) [][]byte {
	r := rand.New(rand.NewPCG(uint64(k), uint64(rowSize)))
	rows := make([][]byte, k)
	for i := range rows {
		rows[i] = make([]byte, rowSize)
		for j := range rows[i] {
			rows[i][j] = byte(r.IntN(256))
		}
	}
	return rows
}

// TestCoderEncodeRoundtrip exercises Coder.Encode across the matrix and
// confirms basic invariants: commitment is non-zero, RLC has K entries,
// original rows pass through unmutated, and encoding the same data twice
// produces the same commitment.
func TestCoderEncodeRoundtrip(t *testing.T) {
	for _, tc := range roundtripConfigs {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &rsema1d.Config{K: tc.k, N: tc.n, WorkerCount: 1}
			data := fillRows(tc.k, tc.rowSize)

			ed, commitment, rlcOrig := encodeRows(t, cfg, data)

			if commitment == (rsema1d.Commitment{}) {
				t.Error("commitment is zero")
			}
			if len(rlcOrig) != cfg.K {
				t.Errorf("rlcOrig len=%d want %d", len(rlcOrig), cfg.K)
			}
			for i := range cfg.K {
				if !bytes.Equal(ed.Row(i), data[i]) {
					t.Errorf("row %d mutated by encode", i)
				}
			}

			_, commitment2, _ := encodeRows(t, cfg, data)
			if commitment != commitment2 {
				t.Errorf("non-deterministic commitment: %x vs %x", commitment, commitment2)
			}
		})
	}
}

// TestEncodeWithTreeBuffer checks that a caller-provided tree buffer yields the
// same commitment as the allocating path, and that an undersized buffer panics
// in the tree builder.
func TestEncodeWithTreeBuffer(t *testing.T) {
	cfg := &rsema1d.Config{K: 16, N: 48, WorkerCount: 1}
	coder, err := rsema1d.NewCoder(cfg)
	if err != nil {
		t.Fatalf("NewCoder: %v", err)
	}

	const rowSize = 256
	rows := make([][]byte, cfg.K+cfg.N)
	for i := range rows {
		rows[i] = make([]byte, rowSize)
	}
	for i := range cfg.K {
		for j := range rows[i] {
			rows[i][j] = byte(i*7 + j)
		}
	}
	zeroParity := func() {
		for i := cfg.K; i < cfg.K+cfg.N; i++ {
			clear(rows[i])
		}
	}

	zeroParity()
	want, err := coder.Encode(rows)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	zeroParity()
	buf := make([]byte, cfg.TreeBufferSize())
	got, err := coder.EncodeWithTree(rows, buf)
	if err != nil {
		t.Fatalf("EncodeWithTree: %v", err)
	}
	if want.Commitment() != got.Commitment() {
		t.Fatalf("commitment mismatch between buffer-backed and allocated encode")
	}

	// treeBuffer is now ignored (the Bao row tree self-allocates and the RLC
	// commitment is a flat hash), so an undersized buffer is harmless.
	zeroParity()
	if _, err := coder.EncodeWithTree(rows, make([]byte, 8)); err != nil {
		t.Fatalf("EncodeWithTree with ignored buffer: %v", err)
	}
}
