package rsema1d

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/bits"
	"runtime"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/merkle"
	"github.com/klauspost/reedsolomon"
)

// rlcLeafSize is the byte size of one GF128 leaf in the padded RLC merkle tree.
const rlcLeafSize = 16

// Verifier batches RLC extension, padded RLC tree construction, row-proof
// merkle verification, batched RLC computation, and commitment checks into
// a single Verify call. The Verifier is bound to a Config at construction
// and reuses internal buffers across Verify calls — making it the right
// shape for hot loops where per-call allocation would otherwise dominate.
//
// A Verifier is not safe for concurrent use; create one per goroutine.
type Verifier struct {
	config *Config
	enc    reedsolomon.Encoder

	// rlcShards holds K+N Leopard-formatted shard views, each a 64-byte
	// slice into one shared backing array (the array is kept alive by the
	// slice headers themselves — no separate field needed).
	rlcShards [][]byte

	// rlcExtended holds the K+N unpacked GF128 values produced by enc.Encode.
	rlcExtended []field.GF128

	// rlcLeaves holds kPadded leaf views: the first K alias 16-byte slots in
	// a shared backing array, the trailing kPadded-K all alias one shared
	// zero slice.
	rlcLeaves [][]byte

	// Per-call grow buffers. Capacity climbs to the largest batch seen and
	// stays there; the slice header is resliced to the current batch length.
	rowsView    [][]byte
	proofInputs []merkle.ProofInput
}

// NewVerifier constructs a Verifier bound to cfg. Reusable buffers sized to
// (K+N) shards and kPadded RLC leaves are allocated up front; the cached
// Reed-Solomon encoder is created once and shared by every Verify call.
func NewVerifier(cfg *Config) (*Verifier, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	enc, err := reedsolomon.New(cfg.K, cfg.N, reedsolomon.WithLeopardGF16(true))
	if err != nil {
		return nil, fmt.Errorf("failed to create encoder: %w", err)
	}

	rlcShardsBuf := make([]byte, (cfg.K+cfg.N)*field.LeopardShardSize)
	rlcShards := make([][]byte, cfg.K+cfg.N)
	for i := range rlcShards {
		rlcShards[i] = rlcShardsBuf[i*field.LeopardShardSize : (i+1)*field.LeopardShardSize]
	}

	rlcLeavesBuf := make([]byte, cfg.K*rlcLeafSize)
	zeroLeaf := make([]byte, rlcLeafSize)
	rlcLeaves := make([][]byte, cfg.kPadded)
	for i := range cfg.K {
		rlcLeaves[i] = rlcLeavesBuf[i*rlcLeafSize : (i+1)*rlcLeafSize]
	}
	for i := cfg.K; i < cfg.kPadded; i++ {
		rlcLeaves[i] = zeroLeaf
	}

	return &Verifier{
		config:      cfg,
		enc:         enc,
		rlcShards:   rlcShards,
		rlcExtended: make([]field.GF128, cfg.K+cfg.N),
		rlcLeaves:   rlcLeaves,
	}, nil
}

// Verify validates a batch of row proofs against commitment using RLS as
// the original K RLC values for the shard. RLC extension, padded RLC tree
// construction, per-row merkle proof verification, batched RLC computation,
// and per-row commitment hashing all happen in one call against the
// Verifier's reusable buffers. Returns the rlc merkle root on success.
func (v *Verifier) Verify(commitment Commitment, rlc []field.GF128, proofs []*RowProof) ([]byte, error) {
	if len(rlc) != v.config.K {
		return nil, fmt.Errorf("expected %d RLC values, got %d", v.config.K, len(rlc))
	}
	if len(proofs) == 0 {
		return nil, errors.New("no proofs provided")
	}

	// pack rlcOrig into both the Leopard shard slab (for RS extension) and the
	// 16-byte leaves slab (for the padded RLC tree). Both source from the same
	// caller-owned slice but each writes into its own pre-allocated backing.
	for i := range v.config.K {
		field.PackToLeopard(rlc[i], v.rlcShards[i])
		b := field.ToBytes128(rlc[i])
		copy(v.rlcLeaves[i], b[:])
	}
	// parity shards must be zeroed before the in-place encode; leftover bytes
	// from the previous Verify would otherwise feed into the systematic encode.
	for i := v.config.K; i < v.config.K+v.config.N; i++ {
		clear(v.rlcShards[i])
	}

	if err := v.enc.Encode(v.rlcShards); err != nil {
		return nil, fmt.Errorf("extending RLC: %w", err)
	}
	for i := range v.config.K + v.config.N {
		v.rlcExtended[i] = field.UnpackFromLeopard(v.rlcShards[i])
	}
	// 16-byte leaves: ALU-bound, not bandwidth-bound, so unrelated to
	// cfg.WorkerCount (which governs the RLC-extension stages). Pinned to
	// 1 because today merkle spawns goroutines per call — running parallel
	// here would compound with the upload pool's own concurrency. Once
	// merkle gets a persistent worker pool the right answer flips: delegate
	// to the pool rather than pin sequential.
	rlcRoot := merkle.NewTreeWithWorkers(v.rlcLeaves, 1).Root()

	expectedProofDepth := bits.Len(uint(v.config.totalPadded)) - 1
	rowSize := v.config.RowSize
	for i, p := range proofs {
		if p == nil {
			return nil, errors.New("received nil proof in verifier")
		}
		if i == 0 && rowSize == 0 {
			rowSize = len(p.Row)
		}
		if p.Index < 0 || p.Index >= v.config.K+v.config.N {
			return nil, fmt.Errorf("index %d out of range [0, %d)", p.Index, v.config.K+v.config.N)
		}
		if v.config.RowSize > 0 && len(p.Row) != v.config.RowSize {
			return nil, fmt.Errorf("row %d: row size mismatch: expected %d, got %d", p.Index, v.config.RowSize, len(p.Row))
		}
		if len(p.Row) != rowSize {
			return nil, fmt.Errorf("batched verify requires equal-sized rows: row %d has %d bytes, expected %d",
				p.Index, len(p.Row), rowSize)
		}
		if len(p.RowProof) != expectedProofDepth {
			return nil, fmt.Errorf("row %d: proof depth mismatch: expected %d, got %d", p.Index, expectedProofDepth, len(p.RowProof))
		}
	}
	// variable-row-size mode (cfg.RowSize == 0) derives rowSize from
	// proofs[0]; guard against shapes computeRLCVectorized cannot accept.
	if rowSize == 0 || rowSize%chunkSize != 0 {
		return nil, fmt.Errorf("row size must be a positive multiple of %d, got %d", chunkSize, rowSize)
	}

	v.proofInputs = resizeProofInputs(v.proofInputs, len(proofs))
	for i, p := range proofs {
		v.proofInputs[i] = merkle.ProofInput{
			Leaf:  p.Row,
			Index: mapIndexToTreePosition(p.Index, v.config),
			Path:  p.RowProof,
		}
	}
	// GOMAXPROCS rather than cfg.WorkerCount: per-row merkle is ALU-bound on
	// cache-resident proof bytes (not memory-bandwidth-bound like the RLC
	// stages), so fanning out doesn't contend with concurrent verifiers in
	// the bounded pool — the win is preserved even when cfg.WorkerCount=1.
	rowRoot, err := merkle.ComputeRootFromProofs(v.proofInputs, runtime.GOMAXPROCS(0))
	if err != nil {
		return nil, fmt.Errorf("verifying row proofs: %w", err)
	}

	// all proofs verified to the same rowRoot; check commitment once.
	h := sha256.New()
	h.Write(rowRoot[:])
	h.Write(rlcRoot[:])
	var commit [32]byte
	h.Sum(commit[:0])
	if commitment != commit {
		return nil, errors.New("commitment verification failed")
	}

	coeffs := deriveCoefficients(rowRoot, v.config.K, v.config.N, rowSize)

	v.rowsView = resizeRows(v.rowsView, len(proofs))
	for i, p := range proofs {
		v.rowsView[i] = p.Row
	}
	computedRLCs := computeRLCVectorized(v.rowsView, coeffs, v.config)

	for i, p := range proofs {
		if !field.Equal128(computedRLCs[i], v.rlcExtended[p.Index]) {
			return nil, fmt.Errorf("row %d: computed RLC does not match expected value", p.Index)
		}
	}

	return rlcRoot[:], nil
}

func resizeRows(buf [][]byte, n int) [][]byte {
	if cap(buf) < n {
		return make([][]byte, n)
	}
	return buf[:n]
}

func resizeProofInputs(buf []merkle.ProofInput, n int) []merkle.ProofInput {
	if cap(buf) < n {
		return make([]merkle.ProofInput, n)
	}
	return buf[:n]
}
