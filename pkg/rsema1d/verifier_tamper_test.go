package rsema1d_test

import (
	"math/rand/v2"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
)

// Verifier tamper rejection tests. Each test corrupts exactly one input
// element (commitment, a proof's Row, a proof's RowProof, the supplied
// rlcOrig, or the proof shape) and confirms Verify returns an error rather
// than silently accepting.

// tamperedSetup encodes a fresh random matrix and returns the Verifier plus
// a 16-proof batch starting at firstProofIndex.
func tamperedSetup(t *testing.T, seed uint64, firstProofIndex int) (*rsema1d.Verifier, rsema1d.Commitment, []*rsema1d.RowProof, []field.GF128) {
	t.Helper()
	cfg := &rsema1d.Config{K: 64, N: 64, WorkerCount: 1}
	v, err := rsema1d.NewVerifier(cfg)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	r := rand.New(rand.NewPCG(seed, seed))
	ed, commitment, rlcOrig := encodeRandom(t, r, cfg, 1024)
	return v, commitment, rangeProofs(t, ed, firstProofIndex, firstProofIndex+16), rlcOrig
}

// TestVerifierRejectsTamperedCommitment flips a bit in the supplied
// commitment so the SHA256(rowRoot||rlcOrigRoot) reconstruction inside verify
// no longer matches.
func TestVerifierRejectsTamperedCommitment(t *testing.T) {
	v, commitment, proofs, rlcOrig := tamperedSetup(t, 1, 0)
	commitment[0] ^= 1
	if _, err := v.Verify(commitment, proofs, rlcOrig); err == nil {
		t.Fatalf("Verify accepted tampered commitment")
	}
}

// TestVerifierRejectsTamperedOriginalRow flips a byte in an original-row's
// data: the RLC computed from the tampered row won't match the extended RLC
// shard the Verifier built from rlcOrig.
func TestVerifierRejectsTamperedOriginalRow(t *testing.T) {
	v, commitment, proofs, rlcOrig := tamperedSetup(t, 2, 0)
	proofs[3].Row = append([]byte(nil), proofs[3].Row...)
	proofs[3].Row[0] ^= 0xFF
	if _, err := v.Verify(commitment, proofs, rlcOrig); err == nil {
		t.Fatalf("Verify accepted tampered original row")
	}
}

// TestVerifierRejectsTamperedParityRow same as the original-row case but
// against a parity (i >= K) row, to confirm the check has no asymmetry.
func TestVerifierRejectsTamperedParityRow(t *testing.T) {
	v, commitment, proofs, rlcOrig := tamperedSetup(t, 3, 64)
	proofs[0].Row = append([]byte(nil), proofs[0].Row...)
	proofs[0].Row[0] ^= 0xFF
	if _, err := v.Verify(commitment, proofs, rlcOrig); err == nil {
		t.Fatalf("Verify accepted tampered parity row")
	}
}

// TestVerifierRejectsTamperedRowProof flips a byte inside a proof's merkle
// path. The recomputed row root won't match what the commitment was built
// against, so the commitment check fails.
func TestVerifierRejectsTamperedRowProof(t *testing.T) {
	v, commitment, proofs, rlcOrig := tamperedSetup(t, 4, 0)
	proofs[3].RowProof = append([][]byte(nil), proofs[3].RowProof...)
	proofs[3].RowProof[0] = append([]byte(nil), proofs[3].RowProof[0]...)
	proofs[3].RowProof[0][0] ^= 1
	if _, err := v.Verify(commitment, proofs, rlcOrig); err == nil {
		t.Fatalf("Verify accepted tampered row proof path")
	}
}

// TestVerifierRejectsTamperedRLC flips a bit in one of the supplied rlcOrig
// values. The Reed-Solomon extension of the tampered vector diverges from
// the committed rlcOrigRoot, so the commitment check fails.
func TestVerifierRejectsTamperedRLC(t *testing.T) {
	v, commitment, proofs, rlcOrig := tamperedSetup(t, 5, 0)
	tampered := make([]field.GF128, len(rlcOrig))
	copy(tampered, rlcOrig)
	tampered[3][0] ^= 1
	if _, err := v.Verify(commitment, proofs, tampered); err == nil {
		t.Fatalf("Verify accepted tampered RLC")
	}
}

// TestVerifierRejectsMultipleTamperedRows confirms the per-row RLC check
// catches every tampered row in a batch, not just the first.
func TestVerifierRejectsMultipleTamperedRows(t *testing.T) {
	v, commitment, proofs, rlcOrig := tamperedSetup(t, 6, 0)
	for _, i := range []int{1, 5, 9} {
		proofs[i].Row = append([]byte(nil), proofs[i].Row...)
		proofs[i].Row[0] ^= 0xFF
	}
	if _, err := v.Verify(commitment, proofs, rlcOrig); err == nil {
		t.Fatalf("Verify accepted multiple tampered rows")
	}
}

// TestVerifierRejectsBadProofDepth supplies a row proof whose merkle path is
// shorter than the expected tree depth, exercising the upfront validateProofs
// shape check.
func TestVerifierRejectsBadProofDepth(t *testing.T) {
	v, commitment, proofs, rlcOrig := tamperedSetup(t, 7, 0)
	proofs[0].RowProof = proofs[0].RowProof[:len(proofs[0].RowProof)-1]
	if _, err := v.Verify(commitment, proofs, rlcOrig); err == nil {
		t.Fatalf("Verify accepted proof with bad depth")
	}
}
