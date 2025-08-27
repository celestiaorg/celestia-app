# RSEMA1D Codec

[![Go Reference](https://pkg.go.dev/badge/github.com/celestiaorg/rsema1d.svg)](https://pkg.go.dev/github.com/celestiaorg/rsema1d)
[![Go Report Card](https://goreportcard.com/badge/github.com/celestiaorg/rsema1d)](https://goreportcard.com/report/github.com/celestiaorg/rsema1d)

RSEMA1D (Reed-Solomon Evans-Mohnblatt-Angeris 1D) is a high-performance data availability codec that provides efficient commitment, proof generation, and verification for vertically-extended data matrices. It uses random linear combinations (RLCs) with 128-bit security against forgery attacks.

## Features

- **Vertical Reed-Solomon Extension**: Efficient encoding using Leopard codec over GF(2^16)
- **Dual Proof System**: Optimized for different verification contexts (DA sampling vs. single row reads)
- **Arbitrary Parameters**: Supports any K and N values (non-power-of-2) with automatic padding
- **128-bit Security**: Uses GF(2^128) for RLC forgery resistance
- **Parallel Processing**: Built-in support for concurrent encoding/verification
- **Efficient Verification**: O(K) operations for extended rows, O(log K) for original rows

## Installation

```bash
go get github.com/celestiaorg/rsema1d
```

## Quick Start

### Basic Encoding

```go
package main

import (
    "fmt"
    "github.com/celestiaorg/rsema1d"
)

func main() {
    // Configure codec parameters
    config := &rsema1d.Config{
        K:       4,    // Number of original rows
        N:       4,    // Number of parity rows
        RowSize: 4096, // Size of each row (must be multiple of 64)
    }

    // Create your data matrix (K rows × RowSize bytes)
    data := make([][]byte, config.K)
    for i := 0; i < config.K; i++ {
        data[i] = make([]byte, config.RowSize)
        // Fill with your data...
    }

    // Encode and generate commitment
    extended, commitment, err := rsema1d.Encode(data, config)
    if err != nil {
        panic(err)
    }

    fmt.Printf("Commitment: %x\n", commitment)
}
```

### Proof Generation and Verification

This implementation provides two verification modes with different trade-offs:
- **Standalone**: Self-contained proofs for original rows, no external dependencies
- **Context-based**: Requires pre-downloaded RLC values but verifies both original and extended rows efficiently

#### Standalone Verification (Single Row Read)

Best for reading individual original rows without additional context:

```go
// Generate standalone proof for an original row
proof, err := extended.GenerateStandaloneProof(rowIndex)
if err != nil {
    panic(err)
}

// Verify the proof (no context needed)
err = rsema1d.VerifyStandaloneProof(proof, commitment, config)
if err != nil {
    panic(err)
}
```

#### Context-Based Verification (DA Sampling)

Efficient for verifying multiple rows with the same commitment. The context pre-computes the RLC tree once, enabling O(K) verification for extended rows instead of requiring individual RLC proofs:

```go
// Create verification context once
context, err := rsema1d.CreateVerificationContext(extended.rlcOrig, config)
if err != nil {
    panic(err)
}

// Generate lightweight proofs
proof, err := extended.GenerateRowProof(rowIndex)
if err != nil {
    panic(err)
}

// Verify with context (works for both original and extended rows)
err = rsema1d.VerifyRowWithContext(proof, commitment, context)
if err != nil {
    panic(err)
}
```

### Data Reconstruction

Recover original data from any K available rows:

```go
// Collect any K rows and their indices
availableRows := [][]byte{row1, row2, row3, row4}
indices := []int{0, 2, 4, 6} // Can be any K indices

// Reconstruct original data
originalData, err := rsema1d.Reconstruct(availableRows, indices, config)
if err != nil {
    panic(err)
}
```

## Configuration

The `Config` struct controls all codec parameters:

```go
type Config struct {
    K           int  // Number of original rows (1 ≤ K ≤ 65536)
    N           int  // Number of parity rows (1 ≤ N ≤ 65536, K+N ≤ 65536)
    RowSize     int  // Bytes per row (must be ≥ 64 and multiple of 64)
    WorkerCount int  // Parallel workers (default: runtime.NumCPU())
}
```

### Parameter Constraints

- **K + N ≤ 65536**: Limited by GF(2^16) field size
- **RowSize**: Must be at least 64 bytes and a multiple of 64 (Leopard codec requirement)
- **Non-power-of-2 support**: K and N can be arbitrary values, padding is handled automatically

## Architecture

### Core Components

- **Field Arithmetic** (`field/`): GF(2^16) and GF(2^128) operations
- **Encoding** (`encoding/`): Leopard Reed-Solomon codec wrapper
- **Merkle Trees** (`merkle/`): Binary trees with RFC 6962-compatible formatting
- **Proof System**: Dual verification paths for different use cases
- **Commitment**: SHA-256 based with Fiat-Shamir coefficient derivation

### Security Properties

- **RLC Forgery Resistance**: 128-bit security via GF(2^128) field
- **Commitment Binding**: SHA-256 collision resistance
- **Proximity Gap**: Reed-Solomon minimum distance of N+1 symbols
- **Encoding Soundness**: Depends on number of random samples verified (application-specific)

## Performance

### Proof Sizes

- **Original rows (standalone)**: `rowSize + O(log(K+N) × 32)` bytes
- **Original rows (with context)**: `rowSize + O(log(K+N) × 32)` bytes
- **Extended rows (with context)**: `rowSize + O(log(K+N) × 32)` bytes
- **Extended rows (standalone)**: Not supported (requires RLC original values)

### Memory Requirements

- **Prover**: O(K × rowSize) for data storage
- **Verifier (original)**: O(rowSize) + O(log(K+N))
- **Verifier (extended)**: O(K × 16) + O(rowSize) + O(log(K+N))

## Use Cases

### 1. Data Availability Sampling
Light clients randomly sample rows to verify data availability:
- Use context-based verification for efficiency
- Download RLC original values once, verify multiple rows

### 2. Rollup Data Retrieval
Full nodes download all original data:
- Use bulk proof functions (when implemented)
- Efficient subtree proofs for K original rows

### 3. Single Row Verification
Applications reading specific rows:
- Use standalone proofs for original rows
- No additional downloads required

## Testing

Run the test suite:

```bash
go test ./...
```

Run with race detection:

```bash
go test -race ./...
```

Generate test vectors:

```bash
go run cmd/testvectors/main.go
```

## Benchmarks

Run performance benchmarks:

```bash
go test -bench=. ./...
```

## Dependencies

- [celestiaorg/reedsolomon](https://github.com/celestiaorg/reedsolomon): Leopard Reed-Solomon codec
- Go standard library (crypto/sha256, encoding/binary)

## Specification

See [SPEC.md](SPEC.md) for the complete technical specification including:
- Mathematical foundations
- Detailed algorithms
- Security analysis
- Test vectors
- Serialization formats

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## References

- [EMA Paper](https://eprint.iacr.org/2025/034): Evans-Mohnblatt-Angeris construction
- [Leopard Codec](https://github.com/catid/leopard): Fast Reed-Solomon implementation
- [RFC 6962](https://tools.ietf.org/html/rfc6962): Certificate Transparency Merkle trees