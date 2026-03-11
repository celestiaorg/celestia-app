package types

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
)

const (
	// ModuleName defines the module name
	ModuleName = "zkism"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// MaxPaginationLimit is the maximum number of items returned in a paginated query.
	MaxPaginationLimit = 100

	// MinStateBytes is the minimumm size of the trusted ism state (32 bytes).
	MinStateBytes = 32

	// MaxStateBytes is the maximum size of the trusted ism state (2 KiB = 2048 bytes).
	MaxStateBytes = 2048

	// MaxStateTransitionValuesBytes is the maximum size permitted for public values payloads associated
	// with state transition proofs.
	// 2 * 8 bytes (length prefixes) + 2 * MaxStateBytes = 5012 bytes
	MaxStateTransitionValuesBytes = (2 * 8) + (2 * MaxStateBytes)

	// MaxMessageIdsCount is the maximum number of message IDs allowed in a StateMembershipValues.
	MaxMessageIdsCount = 1_000_000

	// MaxStateMembershipValuesBytes is the maximum size permitted for public values payloads associated
	// with state membership proofs.
	// Calculated as: 32 (StateRoot) + 32 (MerkleTreeAddress) + 8 (count) + MaxMessageIdsCount * 32 (MessageIds)
	MaxStateMembershipValuesBytes = 32 + 32 + 8 + (MaxMessageIdsCount * 32)

	// groth16VkeyCurvePointsSize is the size of the 6 curve points that
	// precede the G1.K length prefix in a serialized BN254 verifying key.
	// G1.Alpha (32) + G1.Beta (32) + G2.Beta (64) + G2.Gamma (64) +
	// G1.Delta (32) + G2.Delta (64) = 288 bytes.
	groth16VkeyCurvePointsSize = 288

	// Groth16VkeyG1KLength is the expected number of G1.K elements in the
	// verifying key. For the SP1 scheme with 2 public inputs this is
	// nPublic + 1 = 3.
	Groth16VkeyG1KLength = 3

	// Groth16VkeySize is the exact expected size of a serialized Groth16
	// verifying key for the BN254 curve with 2 public inputs (SP1 scheme).
	// Layout: 6 curve points (288 bytes) + uint32 G1.K length (4 bytes) +
	// 3 × compressed G1 points (96 bytes) + uint32 CommitmentKeys length (4 bytes) +
	// uint32 PublicAndCommitmentCommitted length (4 bytes) = 396.
	Groth16VkeySize = 396

	// groth16VkeyCommitmentKeysOffset is the byte offset of the uint32
	// CommitmentKeys length prefix in a serialized BN254 verifying key.
	// 288 (curve points) + 4 (G1.K length) + 96 (3 × G1.K elements) = 388.
	groth16VkeyCommitmentKeysOffset = groth16VkeyCurvePointsSize + 4 + (Groth16VkeyG1KLength * 32)

	// groth16VkeyPubCommittedOffset is the byte offset of the uint32
	// PublicAndCommitmentCommitted length prefix in a serialized BN254 verifying key.
	groth16VkeyPubCommittedOffset = groth16VkeyCommitmentKeysOffset + 4

	// DefaultProofVerifyCostGroth16 is the default gas cost metered for verifying a groth16 proof.
	// NOTE: This is informed by benchmark comparisons with Secp256k1 signature verification.
	// See internal/groth16/bench_test.go
	DefaultProofVerifyCostGroth16 = 6000
)

var (
	IsmsKeyPrefix               = collections.NewPrefix(0)
	MessageKeyPrefix            = collections.NewPrefix(1)
	MessageProofSubmittedPrefix = collections.NewPrefix(2)
)

// ValidateGroth16Vkey checks that a serialized Groth16 verifying key has the
// expected total size and that the internal G1.K length prefix matches the
// expected number of public input commitments. This must be called before
// passing the key to gnark's deserializer to prevent OOM from an inflated
// length prefix.
func ValidateGroth16Vkey(vkey []byte) error {
	if len(vkey) != Groth16VkeySize {
		return fmt.Errorf("groth16 vkey must be exactly %d bytes, got %d", Groth16VkeySize, len(vkey))
	}

	g1kLen := binary.BigEndian.Uint32(vkey[groth16VkeyCurvePointsSize : groth16VkeyCurvePointsSize+4])
	if g1kLen != Groth16VkeyG1KLength {
		return fmt.Errorf("groth16 vkey G1.K length must be %d, got %d", Groth16VkeyG1KLength, g1kLen)
	}

	commitmentKeysLen := binary.BigEndian.Uint32(vkey[groth16VkeyCommitmentKeysOffset : groth16VkeyCommitmentKeysOffset+4])
	if commitmentKeysLen != 0 {
		return fmt.Errorf("groth16 vkey CommitmentKeys length must be 0, got %d", commitmentKeysLen)
	}

	pubCommittedLen := binary.BigEndian.Uint32(vkey[groth16VkeyPubCommittedOffset : groth16VkeyPubCommittedOffset+4])
	if pubCommittedLen != 0 {
		return fmt.Errorf("groth16 vkey PublicAndCommitmentCommitted length must be 0, got %d", pubCommittedLen)
	}

	return nil
}

// EncodeHex is a convenience function to encode byte slices as 0x prefixed hexadecimal strings.
func EncodeHex(bz []byte) string {
	return fmt.Sprintf("0x%s", hex.EncodeToString(bz))
}

// DecodeHex is a convenience function to decode 0x prefixed hexadecimal strings as byte slices.
func DecodeHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")

	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}

	return b, nil
}
