package rsema1d

import (
	"math/rand/v2"
	"testing"
)

// TestVerifyStandaloneProofAcceptsValid runs GenerateStandaloneProof followed
// by VerifyStandaloneProof across the full K/N/RowSize matrix to confirm the
// happy path holds for every shape — original rows only, per spec.
func TestVerifyStandaloneProofAcceptsValid(t *testing.T) {
	for _, tc := range roundtripConfigs {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{K: tc.k, N: tc.n, RowSize: tc.rowSize, WorkerCount: 1}
			ed, commitment, _ := encodeRows(t, cfg, fillRows(tc.k, tc.rowSize))

			// Verify first, middle, and last original rows.
			for _, idx := range []int{0, tc.k / 2, tc.k - 1} {
				proof, err := ed.GenerateStandaloneProof(idx)
				if err != nil {
					t.Fatalf("GenerateStandaloneProof(%d): %v", idx, err)
				}
				if err := VerifyStandaloneProof(proof, commitment, cfg); err != nil {
					t.Fatalf("VerifyStandaloneProof(%d): %v", idx, err)
				}
			}
		})
	}
}

// TestGenerateStandaloneProofRejectsParity confirms the producer side refuses
// to issue standalone proofs for parity rows (Index >= K).
func TestGenerateStandaloneProofRejectsParity(t *testing.T) {
	cfg := &Config{K: 8, N: 8, RowSize: 256, WorkerCount: 1}
	ed, _, _ := encodeRows(t, cfg, fillRows(cfg.K, cfg.RowSize))

	if _, err := ed.GenerateStandaloneProof(cfg.K); err == nil {
		t.Fatalf("GenerateStandaloneProof accepted parity index")
	}
}

// TestVerifyStandaloneProofRejectsTamperedRow flips a byte in the row data;
// the recomputed RLC won't match the RLC merkle leaf, so the commitment
// check fails.
func TestVerifyStandaloneProofRejectsTamperedRow(t *testing.T) {
	cfg := &Config{K: 8, N: 8, RowSize: 256, WorkerCount: 1}
	r := rand.New(rand.NewPCG(31, 31))
	ed, commitment, _ := encodeRows(t, cfg, randomRows(r, cfg.K, cfg.RowSize))

	proof, err := ed.GenerateStandaloneProof(2)
	if err != nil {
		t.Fatalf("GenerateStandaloneProof: %v", err)
	}
	proof.Row = append([]byte(nil), proof.Row...)
	proof.Row[0] ^= 0xFF
	if err := VerifyStandaloneProof(proof, commitment, cfg); err == nil {
		t.Fatalf("VerifyStandaloneProof accepted tampered row")
	}
}

// randomRows fills k rows of `rowSize` random bytes from r.
func randomRows(r *rand.Rand, k, rowSize int) [][]byte {
	rows := make([][]byte, k)
	for i := range rows {
		rows[i] = make([]byte, rowSize)
		for j := range rows[i] {
			rows[i][j] = byte(r.IntN(256))
		}
	}
	return rows
}
