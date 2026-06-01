package rsema1d

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/rlc"
	"github.com/klauspost/reedsolomon"
	"lukechampine.com/blake3"
)

// Verifier owns the reusable state for RLC-based row verification.
// Verify prepares RLC values, root and coefficients once.
// VerifyShared then check batches of row proofs against that cached state.
//
// Verify also reuses scratch buffers and is single-goroutine only.
// VerifyShared allocates scratch buffers per call, so it may run
// concurrently after Verify completes.
type Verifier struct {
	config *Config
	enc    reedsolomon.Encoder

	rlcRoot   [32]byte   // flat BLAKE3 commitment over the K RLC values
	rlcCoeffs rlc.Vector // Fiat-Shamir coefficients for the current matrix.
	rlcShards [][]byte   // Leopard-formatted 64-byte RLC shards

	// Verify scratch buffers. Capacity grows to the largest batch seen.
	// VerifyShared uses per-call locals instead.
	rowsScratch [][]byte
}

// NewVerifier constructs a Verifier bound to cfg. Reusable buffers sized to
// (K+N) shards and the RLC root scratch are allocated up front; the
// cached Reed-Solomon encoder is created once and shared by every Verify call.
func NewVerifier(cfg *Config) (*Verifier, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	workAlloc := newRetainAllocator(leopardEncodeWorkBuffers(cfg.N), field.LeopardChunkSize)
	enc, err := reedsolomon.New(cfg.K, cfg.N,
		reedsolomon.WithLeopardGF16(true),
		reedsolomon.WithWorkAllocator(workAlloc))
	if err != nil {
		return nil, fmt.Errorf("failed to create encoder: %w", err)
	}

	rlcShardsBuf := make([]byte, (cfg.K+cfg.N)*field.LeopardChunkSize)
	rlcShards := make([][]byte, cfg.K+cfg.N)
	for i := range rlcShards {
		rlcShards[i] = rlcShardsBuf[i*field.LeopardChunkSize : (i+1)*field.LeopardChunkSize]
	}

	return &Verifier{
		config:    cfg,
		enc:       enc,
		rlcShards: rlcShards,
	}, nil
}

// Verify checks a batch of row proofs using the given RLC. It checks the
// commitment against the shared row root and the cached RLC commitment, then
// compares each computed row RLC against the extended RLC shard. The RLC is
// cached for the [Verifier.VerifyShared] counterpart.
//
// Verify reuses internal scratch buffers and is not safe for concurrent calls.
func (v *Verifier) Verify(commitment Commitment, proofs []*RowProof, rlc rlc.Vector) error {
	if err := v.setRLC(rlc); err != nil {
		return err
	}

	rowSize, err := v.validateProofs(proofs)
	if err != nil {
		return err
	}
	v.rowsScratch = resizeRows(v.rowsScratch, len(proofs))
	return v.verify(commitment, proofs, rowSize, v.rowsScratch)
}

// VerifyShared is the concurrent-safe counterpart to [Verifier.Verify]. It performs the
// same checks, but uses cached RLC and allocates scratch buffers per call so multiple workers can
// verify independent proof batches against shared RLC state.
//
// Callers that verify many shards against the same RLC vector must call [Verifier.Verify]
// first, then [Verifier.VerifyShared] each shard without repeating the RS
// extension, RLC tree build and coefficients compute.
func (v *Verifier) VerifyShared(commitment Commitment, proofs []*RowProof) error {
	rowSize, err := v.validateProofs(proofs)
	if err != nil {
		return err
	}
	rowsView := make([][]byte, len(proofs))
	return v.verify(commitment, proofs, rowSize, rowsView)
}

// setRLC extends the K original RLC values and caches the RLC commitment.
func (v *Verifier) setRLC(rlc rlc.Vector) error {
	if len(rlc) != v.config.K {
		return fmt.Errorf("expected %d RLC values, got %d", v.config.K, len(rlc))
	}
	// Pack into Leopard shards for RS extension.
	for i := range v.config.K {
		field.GF128ToLeopard(rlc[i], v.rlcShards[i])
	}
	// parity shards must be zeroed before the in-place encode; leftover bytes
	// from the previous Verify would otherwise feed into the systematic encode.
	for i := v.config.K; i < v.config.K+v.config.N; i++ {
		clear(v.rlcShards[i])
	}
	if err := v.enc.Encode(v.rlcShards); err != nil {
		return fmt.Errorf("extending RLC: %w", err)
	}

	v.rlcRoot = rlcCommitment(rlc)
	v.rlcCoeffs = nil // invalidate coeffs
	return nil
}

// validateProofs checks proof shape invariants and returns the effective row
// size, derived from proofs[0].
func (v *Verifier) validateProofs(proofs []*RowProof) (int, error) {
	if len(proofs) == 0 {
		return 0, errors.New("no proofs provided")
	}
	var rowSize int
	var sharedRoot [32]byte
	for i, p := range proofs {
		if p == nil {
			return 0, errors.New("received nil proof in verifier")
		}
		if i == 0 {
			rowSize = len(p.Row)
			sharedRoot = p.RowRoot
		}
		if p.Index < 0 || p.Index >= v.config.K+v.config.N {
			return 0, fmt.Errorf("index %d out of range [0, %d)", p.Index, v.config.K+v.config.N)
		}
		if len(p.Row) != rowSize {
			return 0, fmt.Errorf("batched verify requires equal-sized rows: row %d has %d bytes, expected %d",
				p.Index, len(p.Row), rowSize)
		}
		if p.RowRoot != sharedRoot {
			return 0, fmt.Errorf("row %d: RowRoot disagrees with batch (got %x, want %x)", p.Index, p.RowRoot, sharedRoot)
		}
		if len(p.Slice) == 0 {
			return 0, fmt.Errorf("row %d: empty bao slice", p.Index)
		}
	}
	if rowSize == 0 || rowSize%field.LeopardChunkSize != 0 {
		return 0, fmt.Errorf("row size must be a positive multiple of %d, got %d", field.LeopardChunkSize, rowSize)
	}
	return rowSize, nil
}

// verify is shared by Verify and VerifyShared; callers provide scratch buffers.
// validateProofs has already confirmed that every proof claims the same RowRoot.
func (v *Verifier) verify(commitment Commitment, proofs []*RowProof, rowSize int, rowsView [][]byte) error {
	rowRoot := proofs[0].RowRoot

	// All proofs share rowRoot, so one commitment check covers the batch.
	h := blake3.New(32, nil)
	h.Write(rowRoot[:])
	h.Write(v.rlcRoot[:])
	var commit [32]byte
	h.Sum(commit[:0])
	if commitment != commit {
		return errors.New("commitment verification failed")
	}

	// Per-row Bao slice verification, parallelized across the batch.
	totalRows := v.config.K + v.config.N
	workers := min(gomaxprocs, len(proofs))
	if workers < 1 {
		workers = 1
	}
	chunk := (len(proofs) + workers - 1) / workers
	var firstErr atomic.Value
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		start := w * chunk
		end := min(start+chunk, len(proofs))
		go func(start, end int) {
			defer wg.Done()
			for i := start; i < end; i++ {
				p := proofs[i]
				if !verifyRowSlice(p.Slice, p.Row, p.Index, totalRows, rowRoot) {
					firstErr.CompareAndSwap(nil, fmt.Errorf("row %d: bao slice verification failed", p.Index))
					return
				}
			}
		}(start, end)
	}
	wg.Wait()
	if e := firstErr.Load(); e != nil {
		return e.(error)
	}

	coeffs := v.coefficients(rowRoot, rowSize)

	for i, p := range proofs {
		rowsView[i] = p.Row
	}
	computedRLCs := rlc.Compute(rowsView, coeffs, v.config.WorkerCount)

	for i, p := range proofs {
		expectedRLC := field.GF128FromLeopard(v.rlcShards[p.Index])
		if !field.Equal128(computedRLCs[i], expectedRLC) {
			return fmt.Errorf("row %d: computed RLC does not match expected value", p.Index)
		}
	}
	return nil
}

// coefficients lazily computes Fiat-Shamir coefficients for the current matrix.
func (v *Verifier) coefficients(rowRoot [32]byte, rowSize int) rlc.Vector {
	if v.rlcCoeffs == nil {
		v.rlcCoeffs = rlc.Derive(rowRoot, v.config.K, v.config.N, rowSize, v.config.WorkerCount)
	}
	return v.rlcCoeffs
}

func resizeRows(buf [][]byte, n int) [][]byte {
	if cap(buf) < n {
		return make([][]byte, n)
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

var gomaxprocs = runtime.GOMAXPROCS(0)
