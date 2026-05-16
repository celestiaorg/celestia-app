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

// Verifier owns the reusable state for RLC-based row verification. SetRLC
// prepares the RLC extension and root once; Verify and VerifyShared then check
// batches of row proofs against that cached state.
//
// SetRLC mutates shared state and must not race with verification. Verify also
// reuses scratch buffers and is single-goroutine only. VerifyShared allocates
// scratch buffers per call, so it may run concurrently after SetRLC completes.
type Verifier struct {
	config *Config
	enc    reedsolomon.Encoder

	// K+N Leopard-formatted 64-byte shard views in one backing array. The
	// first K shards are filled from the caller's RLC vector; the rest are
	// Reed-Solomon parity.
	rlcShards [][]byte

	// kPadded 16-byte leaves for the padded RLC tree. The first K leaves alias
	// a shared backing array; padded leaves share one zero leaf.
	rlcLeaves [][]byte

	// Verify scratch buffers. Capacity grows to the largest batch seen.
	// VerifyShared uses per-call locals instead.
	rowsView    [][]byte
	proofInputs []merkle.ProofInput

	rlcRoot [32]byte
}

// NewVerifier constructs a Verifier bound to cfg. Reusable buffers sized to
// (K+N) shards and kPadded RLC leaves are allocated up front; the cached
// Reed-Solomon encoder is created once and shared by every Verify call.
func NewVerifier(cfg *Config) (*Verifier, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	workAlloc := newRetainAllocator(leopardEncodeWorkBuffers(cfg.N), field.LeopardShardSize)
	enc, err := reedsolomon.New(cfg.K, cfg.N,
		reedsolomon.WithLeopardGF16(true),
		reedsolomon.WithWorkAllocator(workAlloc))
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
		config:    cfg,
		enc:       enc,
		rlcShards: rlcShards,
		rlcLeaves: rlcLeaves,
	}, nil
}

// SetRLC extends the K original RLC values and caches the padded RLC root.
// Callers that verify many shards against the same RLC vector can call SetRLC
// once, then Verify or VerifyShared each shard without repeating the RS
// extension and RLC tree build.
func (v *Verifier) SetRLC(rlc []field.GF128) error {
	if len(rlc) != v.config.K {
		return fmt.Errorf("expected %d RLC values, got %d", v.config.K, len(rlc))
	}
	// Pack once into both layouts needed below: Leopard shards for RS
	// extension and serialized leaves for the padded RLC tree.
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
		return fmt.Errorf("extending RLC: %w", err)
	}
	// Keep this sequential: callers often run several Verifiers in parallel.
	v.rlcRoot = merkle.NewTreeWithWorkers(v.rlcLeaves, 1).Root()
	return nil
}

// RLCRoot returns the padded RLC merkle root from the latest SetRLC call.
func (v *Verifier) RLCRoot() []byte {
	return v.rlcRoot[:]
}

// Verify checks a batch of row proofs using cached RLC state from SetRLC. It
// verifies the shared row root, checks the commitment, then compares each
// computed row RLC against the extended RLC shard.
//
// Verify reuses internal scratch buffers and is not safe for concurrent calls.
func (v *Verifier) Verify(commitment Commitment, proofs []*RowProof) error {
	rowSize, err := v.validateProofs(proofs)
	if err != nil {
		return err
	}
	v.rowsView = resizeRows(v.rowsView, len(proofs))
	v.proofInputs = resizeProofInputs(v.proofInputs, len(proofs))
	return v.verify(commitment, proofs, rowSize, v.rowsView, v.proofInputs)
}

// VerifyShared is the concurrent-safe counterpart to Verify. It performs the
// same checks, but allocates scratch buffers per call so multiple workers can
// verify independent proof batches against one prepared RLC state.
func (v *Verifier) VerifyShared(commitment Commitment, proofs []*RowProof) error {
	rowSize, err := v.validateProofs(proofs)
	if err != nil {
		return err
	}
	rowsView := make([][]byte, len(proofs))
	proofInputs := make([]merkle.ProofInput, len(proofs))
	return v.verify(commitment, proofs, rowSize, rowsView, proofInputs)
}

// validateProofs checks proof shape invariants and returns the effective row
// size, which is derived from proofs[0] when cfg.RowSize is unset.
func (v *Verifier) validateProofs(proofs []*RowProof) (int, error) {
	if len(proofs) == 0 {
		return 0, errors.New("no proofs provided")
	}
	expectedProofDepth := bits.Len(uint(v.config.totalPadded)) - 1
	rowSize := v.config.RowSize
	for i, p := range proofs {
		if p == nil {
			return 0, errors.New("received nil proof in verifier")
		}
		if i == 0 && rowSize == 0 {
			rowSize = len(p.Row)
		}
		if p.Index < 0 || p.Index >= v.config.K+v.config.N {
			return 0, fmt.Errorf("index %d out of range [0, %d)", p.Index, v.config.K+v.config.N)
		}
		if v.config.RowSize > 0 && len(p.Row) != v.config.RowSize {
			return 0, fmt.Errorf("row %d: row size mismatch: expected %d, got %d", p.Index, v.config.RowSize, len(p.Row))
		}
		if len(p.Row) != rowSize {
			return 0, fmt.Errorf("batched verify requires equal-sized rows: row %d has %d bytes, expected %d",
				p.Index, len(p.Row), rowSize)
		}
		if len(p.RowProof) != expectedProofDepth {
			return 0, fmt.Errorf("row %d: proof depth mismatch: expected %d, got %d", p.Index, expectedProofDepth, len(p.RowProof))
		}
	}
	// Variable-row-size mode derives rowSize from proofs[0].
	if rowSize == 0 || rowSize%chunkSize != 0 {
		return 0, fmt.Errorf("row size must be a positive multiple of %d, got %d", chunkSize, rowSize)
	}
	return rowSize, nil
}

// verify is shared by Verify and VerifyShared; callers provide scratch buffers.
func (v *Verifier) verify(commitment Commitment, proofs []*RowProof, rowSize int, rowsView [][]byte, proofInputs []merkle.ProofInput) error {
	for i, p := range proofs {
		proofInputs[i] = merkle.ProofInput{
			Leaf:  p.Row,
			Index: mapIndexToTreePosition(p.Index, v.config),
			Path:  p.RowProof,
		}
	}
	// Per-row Merkle verification is ALU-bound; use process-wide parallelism.
	rowRoot, err := merkle.ComputeRootFromProofs(proofInputs, runtime.GOMAXPROCS(0))
	if err != nil {
		return fmt.Errorf("verifying row proofs: %w", err)
	}

	// All proofs share rowRoot, so one commitment check covers the batch.
	h := sha256.New()
	h.Write(rowRoot[:])
	h.Write(v.rlcRoot[:])
	var commit [32]byte
	h.Sum(commit[:0])
	if commitment != commit {
		return errors.New("commitment verification failed")
	}

	coeffs := deriveCoefficients(rowRoot, v.config.K, v.config.N, rowSize)

	for i, p := range proofs {
		rowsView[i] = p.Row
	}
	computedRLCs := computeRLCVectorized(rowsView, coeffs, v.config)

	for i, p := range proofs {
		expectedRLC := field.UnpackFromLeopard(v.rlcShards[p.Index])
		if !field.Equal128(computedRLCs[i], expectedRLC) {
			return fmt.Errorf("row %d: computed RLC does not match expected value", p.Index)
		}
	}
	return nil
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

// leopardEncodeWorkBuffers returns the number of scratch slices requested by
// klauspost/reedsolomon's GF(2^16) Leopard Encode path: 2*ceilPow2(parity).
func leopardEncodeWorkBuffers(parity int) int {
	return 2 * nextPowerOfTwo(parity)
}

// retainAllocator is a fixed-size reedsolomon.WorkAllocator. The verifier only
// uses the Leopard Encode path on 64-byte RLC shards, so the requested shape is
// invariant after construction. Keeping the slab in a strong reference avoids
// sync.Pool eviction under GC pressure.
type retainAllocator struct {
	work [][]byte
}

func newRetainAllocator(n, size int) *retainAllocator {
	return &retainAllocator{
		work: reedsolomon.AllocAligned(n, size),
	}
}

func (a *retainAllocator) Get(n, _ int) [][]byte {
	return a.work[:n]
}

func (a *retainAllocator) Put([][]byte) {}
