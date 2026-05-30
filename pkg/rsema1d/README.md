# RSEMA1D Codec

RSEMA1D (Reed-Solomon Evans-Mohnblatt-Angeris 1D) is a high-performance data availability codec that provides efficient commitment, proof generation, and verification for vertically-extended data matrices. It uses random linear combinations (RLCs) with 128-bit security against forgery attacks.

## Features

- **Vertical Reed-Solomon Extension**: Efficient encoding using Leopard codec over GF(2^16)
- **Dual Proof System**: Optimized for different verification contexts (DA sampling vs. single row reads)
- **Arbitrary Parameters**: Supports any K and N values (non-power-of-2) with automatic padding
- **128-bit Security**: Uses GF(2^128) for RLC forgery resistance
- **Parallel Processing**: Built-in support for concurrent encoding/verification
- **Efficient Verification**: O(K) operations for extended rows, O(log K) for original rows

## Quick Start

### Basic Encoding

```go
package main

import (
    "fmt"
    "github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
)

func main() {
    // Configure codec parameters. Row size is decided per-blob from the
    // data buffer you hand to Coder.Encode — the same Config can drive
    // blobs of different widths.
    config := &rsema1d.Config{
        K: 4, // Number of original rows
        N: 4, // Number of parity rows
    }
    const rowSize = 4096 // multiple of 64 (one Leopard chunk)

    coder, err := rsema1d.NewCoder(config)
    if err != nil {
        panic(err)
    }

    // Coder.Encode takes a K+N-row buffer: data in rows[:K], zeroed
    // parity slots in rows[K:K+N].
    rows := make([][]byte, config.K+config.N)
    for i := range config.K {
        rows[i] = make([]byte, rowSize)
        // ... fill row i with your data ...
    }
    for i := config.K; i < config.K+config.N; i++ {
        rows[i] = make([]byte, rowSize)
    }

    extended, err := coder.Encode(rows)
    if err != nil {
        panic(err)
    }

    fmt.Printf("Commitment: %x\n", extended.Commitment())
    fmt.Printf("RLC values:  %d\n", len(extended.RLC()))
}
```

### Proof Generation and Verification

This implementation provides two verification modes with different trade-offs:

- **Standalone**: Self-contained proofs for original rows, no external dependencies
- **Batched (Verifier)**: Requires the original RLC vector, verifies many rows (original or parity) against the same commitment with a single shared-state pass

#### Standalone Verification (Single Row Read)

Best for reading individual original rows without additional context:

```go
// Generate a self-contained proof for an original row (index < K).
proof, err := extended.GenerateStandaloneProof(rowIndex)
if err != nil {
    panic(err)
}

// Verify with no external dependencies.
err = rsema1d.VerifyStandaloneProof(proof, extended.Commitment(), config)
if err != nil {
    panic(err)
}
```

#### Batched Verification (DA Sampling)

The [`Verifier`] caches RLC root and coefficients once, then verifies batches of row proofs against that cached state. Best for verifying many rows from the same encoding.

```go
verifier, err := rsema1d.NewVerifier(config)
if err != nil {
    panic(err)
}

// Build proofs for the rows you want to verify (originals or parity).
proofs := make([]*rsema1d.RowProof, n)
for i := range proofs {
    proofs[i], err = extended.GenerateRowProof(indices[i])
    if err != nil {
        panic(err)
    }
}

// Verify the batch against the commitment using the original RLC vector.
// rlcOrigRoot is returned so the caller can pin it for cross-checks.
rlcOrigRoot, err := verifier.Verify(extended.Commitment(), proofs, extended.RLC())
if err != nil {
    panic(err)
}

// Subsequent disjoint batches against the same commitment can skip the
// rlcOrig argument and run concurrently:
err = verifier.VerifyShared(extended.Commitment(), moreProofs)
```

### Data Reconstruction

Recover the K original rows from any K-sized selection of the K+N extended shards. The [`Reconstructor`] streams in RLC-verified row proofs from many sources and rebuilds the originals once enough unique indices have arrived:

```go
reconstructor, err := coder.NewReconstructor(extended.Commitment())
if err != nil {
    panic(err)
}

// Add proof batches as they arrive from peers; Add is safe to call
// concurrently and dedups by Index internally.
for _, batch := range proofBatches {
    novel, err := reconstructor.Add(batch.proofs, batch.rlc)
    if err != nil {
        panic(err)
    }
    for _, p := range novel {
        rows[p.Index] = p.Row
    }
}

if reconstructor.Want() == 0 {
    if err := reconstructor.Reconstruct(rows); err != nil {
        panic(err)
    }
}
```

For in-process reconstruction with all rows already on hand, [`Coder.Reconstruct`] takes a K+N row buffer (missing rows as nil) and rebuilds the originals in place.

## Configuration

The `Config` struct controls all codec parameters:

```go
type Config struct {
    K           int  // Number of original rows (1 ≤ K ≤ 65536)
    N           int  // Number of parity rows (1 ≤ N ≤ 65536, K+N ≤ 65536)
    WorkerCount int  // Parallel workers (default: runtime.NumCPU())
}
```

Row size is not in Config — every operation (Encode, Verify, VerifyStandaloneProof) reads it from the input rows or proofs, so a single Config drives blobs of varying width.

### Parameter Constraints

- **K + N ≤ 65536**: Limited by GF(2^16) field size
- **Row size**: Must be at least 64 bytes and a multiple of 64 (Leopard codec requirement)
- **Non-power-of-2 support**: K and N can be arbitrary values, padding is handled automatically

## Architecture

### Core Components

- **Field Arithmetic** (`field/`): GF(2^16) and GF(2^128) primitives plus Leopard byte-layout codec
- **Random Linear Combination** (`rlc/`): Fiat-Shamir coefficient derivation (`rlc.Derive`), batched vectorized RLC compute (`rlc.Compute`), single-row scalar (`rlc.ComputeRow`), and RLC vector serialization (`rlc.Marshal` / `Unmarshal` and the `rlc.Vector` type)
- **Merkle Trees** (`merkle/`): Binary trees with RFC 6962-compatible formatting; `merkle.Root` is the canonical 32-byte hash type
- **Coder** (`coder.go`): Producer-side encoding and reconstruction with a cached Reed-Solomon encoder
- **Verifier** (`verifier.go`): Batched proof verification with reusable scratch and a concurrent-safe `VerifyShared` path
- **Reconstructor** (`reconstructor.go`): Download-side row collection with RLC verification and dedup
- **Standalone proofs** (`standalone_proof.go`): Self-contained single-row verification for callers without `rlcOrig`

### Security Properties

- **RLC Forgery Resistance**: 128-bit security via GF(2^128) field
- **Commitment Binding**: SHA-256 collision resistance
- **Proximity Gap**: Reed-Solomon minimum distance of N+1 symbols
- **Encoding Soundness**: Depends on number of random samples verified (application-specific)

## Performance

### Proof Sizes

- **Original rows (standalone)**: `rowSize + O(log(K+N) × 32) + O(log(K) × 32)` bytes
- **Original or parity rows (batched)**: `rowSize + O(log(K+N) × 32)` bytes per proof, plus one shared `K × 16`-byte rlcOrig vector

### Memory Requirements

- **Prover**: O(K × rowSize) for data storage
- **Verifier (batched)**: O(K × 16) for the cached RLC shards + O(rowSize × batchSize) scratch
- **Verifier (standalone)**: O(rowSize) + O(log(K+N))

## Use Cases

### 1. Data Availability Sampling

Light clients randomly sample rows to verify data availability:

- Use the [`Verifier`] for batched verification — download `rlcOrig` once, call `Verify` to prime, then `VerifyShared` for subsequent disjoint batches (concurrent-safe).

### 2. Rollup Data Retrieval

Full nodes download all original data:

- Use the [`Reconstructor`] to stream proofs from peers, dedup, and rebuild K originals via Reed-Solomon once enough have arrived.

### 3. Single Row Verification

Applications reading specific original rows without `rlcOrig`:

- Use [`StandaloneProof`] — adds an `rlcProof` linking the row's RLC value to `rlcOrigRoot`, so no external state is needed beyond the commitment.

## Testing

Run the test suite:

```bash
go test ./pkg/rsema1d/...
```

Run with race detection:

```bash
go test -race ./pkg/rsema1d/...
```

Generate test vectors:

```bash
go run ./pkg/rsema1d/cmd/testvectors/main.go
```

## Benchmarks

Run performance benchmarks:

```bash
go test -bench=. ./pkg/rsema1d/...
```

## Dependencies

- [klauspost/reedsolomon](https://github.com/klauspost/reedsolomon): Leopard Reed-Solomon codec
- Go standard library (crypto/sha256, encoding/binary)

## References

- [EMA Paper](https://eprint.iacr.org/2025/034): Evans-Mohnblatt-Angeris construction
- [Leopard Codec](https://github.com/catid/leopard): Fast Reed-Solomon implementation
- [RFC 6962](https://tools.ietf.org/html/rfc6962): Certificate Transparency Merkle trees
