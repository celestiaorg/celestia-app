package fibre

import (
	"crypto/sha256"
	"fmt"
	"math"
	"math/bits"

	cmtmath "github.com/cometbft/cometbft/libs/math"
)

// ProtocolParams defines the fundamental protocol constants from which all other
// configuration values are derived. This provides a single source of truth for
// protocol parameterization.
//
// The design separates "root constants" (what we chose) from "derived values"
// (what follows from our choices). This enables:
//   - Clear documentation of protocol design decisions
//   - Single source of truth for protocol constants
//   - Easy versioning
//   - Testing with non-default parameter values
type ProtocolParams struct {
	// Erasure coding parameters
	//
	// Rows is the number of original data rows (K in rsema1d).
	Rows int
	// EncodingRatio is K/(K+N), the fraction of total rows that are original.
	// For example, 0.25 means 1/4 of total rows are original (so 3/4 are parity).
	EncodingRatio float64

	// Network parameters
	//
	// MaxValidatorCount is the maximum expected number of validators in the network.
	// Used to compute default values for MaxMessageSize and concurrency limits.
	MaxValidatorCount int

	// Security parameters
	//
	// UniqueDecodingSecurityBits defines how likely it is for unique decoding to fail.
	UniqueDecodingSecurityBits int
	// SafetyThreshold is the fraction of voting power required for safety (typically 2/3).
	// The minimum percentage of stake needed to cause a safety failure.
	SafetyThreshold cmtmath.Fraction
	// LivenessThreshold is the fraction of validators needed for reconstruction (typically 1/3).
	// The minimum percentage of stake needed to cause a liveness failure.
	LivenessThreshold cmtmath.Fraction

	// Size constraints
	//
	// MaxBlobSize is the maximum allowed blob size in bytes (including any headers).
	MaxBlobSize int
	// MinRowSize is the minimum row size in bytes (rows are rounded up to this).
	MinRowSize int
}

// DefaultProtocolParams contains the default protocol parameters for version 0.
var DefaultProtocolParams = ProtocolParams{
	Rows:          1 << 12, // 4096 original rows
	EncodingRatio: 0.25,    // 3x parity (12288 parity rows, 16384 total)

	MaxValidatorCount: 100,

	UniqueDecodingSecurityBits: 100,
	SafetyThreshold:            cmtmath.Fraction{Numerator: 2, Denominator: 3},
	LivenessThreshold:          cmtmath.Fraction{Numerator: 1, Denominator: 3},

	MaxBlobSize: 1 << 27, // 128 MiB
	MinRowSize:  1 << 6,  // 64 byte
}

func init() {
	p := DefaultProtocolParams
	livenessRatio := float64(p.LivenessThreshold.Numerator) / float64(p.LivenessThreshold.Denominator)
	if livenessRatio < p.EncodingRatio {
		panic("LivenessThreshold must always be bigger than EncodingRatio as we cannot disperse samples without overlap")
	}
}

// TotalRows returns the total number of rows (K + N).
func (p ProtocolParams) TotalRows() int {
	return int(float64(p.Rows) / p.EncodingRatio)
}

// ParityRows returns the number of parity rows (N in rsema1d).
func (p ProtocolParams) ParityRows() int {
	return p.TotalRows() - p.Rows
}

// RowsPerShard returns the number of rows each shard receives.
// This is the security-optimal number based on:
//  1. Unique decode samples needed for cryptographic security
//  2. Reconstruction samples needed for fault tolerance
func (p ProtocolParams) RowsPerShard(totalShards int) int {
	// Constraint 1: Unique decoding security
	//
	// The minimum number of samples s required for λ bits of security:
	//
	//              ⌈      λ          ⌉
	//         s ≥  | ─────────────── |
	//              ⌈ 1 - log₂(1 + ρ) ⌉
	//
	// Where:
	//   λ (lambda) = UniqueDecodingSecurityBits
	//   ρ (rho)    = EncodingRatio = K/(K+N)
	//
	uniqueDecodeSamples := int(math.Ceil(float64(p.UniqueDecodingSecurityBits) / (1 - math.Log2(1+p.EncodingRatio))))

	// Constraint 2: Reconstruction samples for fault tolerance
	// We need enough rows from LivenessThreshold fraction of shards to reconstruct
	shardsForReconstruction := p.ShardsForReconstruction(totalShards)
	reconstructionSamples := ceilDiv(p.Rows, shardsForReconstruction)

	return max(uniqueDecodeSamples, reconstructionSamples)
}

// ShardsForReconstruction returns the minimum number of shards
// needed to successfully reconstruct the original data.
// Returns at least 1.
func (p ProtocolParams) ShardsForReconstruction(totalShards int) int {
	num := int(p.LivenessThreshold.Numerator)
	den := int(p.LivenessThreshold.Denominator)
	return max(1, ceilDiv(totalShards*num, den))
}

// RowSize computes the row size for the given blob version and total length.
// Row size is calculated as ceil(totalLen / Rows), rounded up to MinRowSize.
// Panics if the blob version is not supported.
func (p ProtocolParams) RowSize(blobVersion uint8, totalLen int) int {
	if blobVersion != 0 {
		panic(fmt.Sprintf("unsupported blob version: %d", blobVersion))
	}

	if totalLen == 0 {
		return 0
	}

	rowSize := ceilDiv(totalLen, p.Rows)

	// Round up to nearest multiple of MinRowSize
	if rowSize%p.MinRowSize != 0 {
		rowSize = ((rowSize / p.MinRowSize) + 1) * p.MinRowSize
	}

	return rowSize
}

// MaxRowSize returns the maximum row size based on MaxBlobSize.
func (p ProtocolParams) MaxRowSize(blobVersion uint8) int {
	return p.RowSize(blobVersion, p.MaxBlobSize)
}

// MaxShardSize calculates the maximum size of a shard in bytes.
// A shard contains: RLC coefficients + (rows_per_shard * (row_index + row_data + merkle_proof))
func (p ProtocolParams) MaxShardSize(totalShards int) int {
	const (
		rowIndexSize = 4  // uint32 index per row
		rlcCoeffSize = 16 // uint128 coefficient per row
	)

	totalRows := p.TotalRows()
	maxRowSize := p.MaxRowSize(0) // version 0 is the only supported version
	rlcCoeffsSize := p.Rows * rlcCoeffSize

	// calculate merkle tree depth for inclusion proofs: ceil(log2(totalRows))
	treeDepth := bits.Len(uint(totalRows - 1))
	proofSizePerRow := treeDepth * sha256.Size

	// rows per shard based on how Assign distributes total rows among shards
	rowsPerShard := ceilDiv(totalRows, totalShards)
	return rlcCoeffsSize + (rowsPerShard * (rowIndexSize + maxRowSize + proofSizePerRow))
}

// MaxMessageSize returns the maximum gRPC message size for upload requests.
// Includes MaxShardSize, MaxPaymentPromiseSize, and 2% protobuf overhead.
func (p ProtocolParams) MaxMessageSize(totalShards int) int {
	msgSize := p.MaxShardSize(totalShards) + MaxPaymentPromiseSize
	return msgSize + (msgSize / 50) // add 2% protobuf overhead
}

// ceilDiv returns ceil(a/b) using integer arithmetic.
func ceilDiv(a, b int) int {
	return (a + b - 1) / b
}
