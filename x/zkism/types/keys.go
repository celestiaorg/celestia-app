package types

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"cosmossdk.io/collections"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
)

const (
	// ModuleName defines the module name
	ModuleName = "zkism"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// MinStateBytes is the minimum size of the trusted ism state (32 bytes).
	MinStateBytes = 32

	// MaxStateBytes is the maximum size of the trusted ism state (2 KiB = 2048 bytes).
	MaxStateBytes = 2048

	// MaxStateTransitionValuesBytes is the maximum size permitted for public values payloads associated
	// with state transition proofs.
	// 2 * 8 bytes (length prefixes) + 2 * MaxStateBytes = 5012 bytes
	MaxStateTransitionValuesBytes = (2 * 8) + (2 * MaxStateBytes)

	// StateMembershipValuesVersion is the layout version of state membership public values. The membership
	// program commits it as the first byte so that public values produced by any other program version are
	// rejected before the proof is verified.
	StateMembershipValuesVersion = 1

	// StateMembershipValuesBytes is the exact size of public values payloads associated
	// with state membership proofs.
	// Calculated as: 1 (Version) + 32 (StateRoot) + 32 (MerkleTreeAddress) + 32 (TreeRoot) + 4 (Count)
	// This can never equal the length of a legacy message id payload, which is 72 + 32 * n bytes.
	StateMembershipValuesBytes = 1 + 32 + 32 + 32 + 4

	// MerkleProofMetadataBytes is the exact size of the metadata provided when verifying a message.
	// Calculated as: 4 (leaf index) + TreeDepth * 32 (merkle branch)
	MerkleProofMetadataBytes = 4 + (util.TreeDepth * 32)

	// groth16VkeyCurvePointsSize is the size of the 6 curve points that
	// precede the G1.K length prefix in a serialized BN254 verifying key.
	// G1.Alpha (32) + G1.Beta (32) + G2.Beta (64) + G2.Gamma (64) +
	// G1.Delta (32) + G2.Delta (64) = 288 bytes.
	groth16VkeyCurvePointsSize = 288

	// groth16PointCompressionMask isolates the two most significant bits of a
	// serialized BN254 curve point, which encode whether the point is
	// compressed. Uncompressed points have both bits clear.
	groth16PointCompressionMask = 0b11 << 6

	// Groth16VkeyG1KLength is the expected number of G1.K elements in the
	// verifying key. For the SP1 v6 scheme with 5 public inputs (vkey_hash,
	// committed_values_digest, exit_code, vk_root, proof_nonce) this is
	// nPublic + 1 = 6.
	Groth16VkeyG1KLength = 6

	// Groth16VkeySize is the exact expected size of a serialized Groth16
	// verifying key for the BN254 curve with 5 public inputs (SP1 v6 scheme).
	// Layout: 6 curve points (288 bytes) + uint32 G1.K length (4 bytes) +
	// 6 × compressed G1 points (192 bytes) + uint32 PublicAndCommitmentCommitted
	// length (4 bytes) + uint32 CommitmentKeys length (4 bytes) = 492.
	Groth16VkeySize = 492

	// legacyGroth16VkeyG1KLength is the number of G1.K elements in an SP1 v5
	// verifying key (2 public inputs). Only genesis validation accepts it, for
	// ISMs created before the SP1 v6 verifier.
	legacyGroth16VkeyG1KLength = 3

	// DefaultRootHistorySize is used when an ISM's root history size is 0.
	DefaultRootHistorySize = 64

	// MaxRootHistorySize bounds the history scanned by Verify and SubmitMessages.
	MaxRootHistorySize = 256

	// DefaultProofVerifyCostGroth16 is the default gas cost metered for verifying a groth16 proof.
	// NOTE: This is informed by benchmark comparisons with Secp256k1 signature verification.
	// See internal/groth16/bench_test.go
	DefaultProofVerifyCostGroth16 = 6000
)

var (
	IsmsKeyPrefix = collections.NewPrefix(0)
	// Prefix 1 held authorized message IDs before messages were verified against the proven merkle tree root (v11).
	// The entries are left in place because nothing reads them and deleting an attacker-sized set in the upgrade
	// block is unbounded work. Do not reuse prefix 1.
	// Prefix 2 held the legacy per-state-root submission flag; entries are left in place. Do not reuse prefix 2.
	MerkleTreeStateKeyPrefix = collections.NewPrefix(3)
	StateRootHistoryPrefix   = collections.NewPrefix(4)
	TreeStateHistoryPrefix   = collections.NewPrefix(5)

	// groth16VkeyPointOffsets are the byte offsets of the six leading curve
	// points (G1.Alpha, G1.Beta, G2.Beta, G2.Gamma, G1.Delta, G2.Delta) in a
	// serialized compressed BN254 verifying key. G1 points are 32 bytes and G2
	// points are 64 bytes.
	groth16VkeyPointOffsets = []int{0, 32, 64, 128, 192, 224}
)

// ValidateGroth16Vkey checks that a serialized Groth16 verifying key has the
// expected total size and that the internal length prefixes (G1.K,
// PublicAndCommitmentCommitted, and CommitmentKeys) have their expected values.
// This must be called before passing the key to gnark's deserializer to
// prevent OOM from inflated length prefixes.
func ValidateGroth16Vkey(vkey []byte) error {
	return validateGroth16VkeyLayout(vkey, Groth16VkeyG1KLength)
}

// ValidateGenesisGroth16Vkey is ValidateGroth16Vkey for an imported ISM. It
// also accepts the SP1 v5 layout with legacyGroth16VkeyG1KLength G1.K
// elements, which ISMs created before the SP1 v6 verifier still hold, with the
// same checks. MsgCreateInterchainSecurityModule keeps the SP1 v6 layout only.
func ValidateGenesisGroth16Vkey(vkey []byte) error {
	if len(vkey) == groth16VkeySize(legacyGroth16VkeyG1KLength) {
		return validateGroth16VkeyLayout(vkey, legacyGroth16VkeyG1KLength)
	}
	return ValidateGroth16Vkey(vkey)
}

// groth16VkeySize returns the size of a serialized compressed BN254 verifying
// key with g1kLen G1.K elements and no commitments: the curve points, the G1.K
// length prefix and elements, and the two empty commitment length prefixes.
func groth16VkeySize(g1kLen int) int {
	return groth16VkeyCurvePointsSize + 4 + g1kLen*32 + 4 + 4
}

// validateGroth16VkeyLayout checks that vkey is a compressed BN254 verifying key
// with exactly g1kLen G1.K elements and no commitments.
func validateGroth16VkeyLayout(vkey []byte, g1kLen int) error {
	if size := groth16VkeySize(g1kLen); len(vkey) != size {
		return fmt.Errorf("groth16 vkey must be exactly %d bytes, got %d", size, len(vkey))
	}

	// The length-prefix offsets below are only correct when every leading curve
	// point uses the compressed encoding. gnark derives each point's width from
	// its own compression-flag byte, so an uncompressed leading point would shift
	// gnark's length reads past these fixed offsets, letting an inflated length
	// slip through and trigger an unbounded allocation during deserialization.
	for _, offset := range groth16VkeyPointOffsets {
		if vkey[offset]&groth16PointCompressionMask == 0 {
			return fmt.Errorf("groth16 vkey curve point at offset %d must be compressed", offset)
		}
	}

	gotG1KLen := binary.BigEndian.Uint32(vkey[groth16VkeyCurvePointsSize : groth16VkeyCurvePointsSize+4])
	if gotG1KLen != uint32(g1kLen) {
		return fmt.Errorf("groth16 vkey G1.K length must be %d, got %d", g1kLen, gotG1KLen)
	}

	// gnark reads PublicAndCommitmentCommitted right after G1.K, then the
	// CommitmentKeys count.
	pubCommittedOffset := groth16VkeyCurvePointsSize + 4 + g1kLen*32
	pubCommittedLen := binary.BigEndian.Uint32(vkey[pubCommittedOffset : pubCommittedOffset+4])
	if pubCommittedLen != 0 {
		return fmt.Errorf("groth16 vkey PublicAndCommitmentCommitted length must be 0, got %d", pubCommittedLen)
	}

	commitmentKeysOffset := pubCommittedOffset + 4
	commitmentKeysLen := binary.BigEndian.Uint32(vkey[commitmentKeysOffset : commitmentKeysOffset+4])
	if commitmentKeysLen != 0 {
		return fmt.Errorf("groth16 vkey CommitmentKeys length must be 0, got %d", commitmentKeysLen)
	}

	return nil
}

// EffectiveRootHistorySize returns the number of replaced roots an ISM with the given configured size keeps.
func EffectiveRootHistorySize(size uint32) uint32 {
	if size == 0 {
		return DefaultRootHistorySize
	}
	return size
}

// ValidateRootHistorySize checks that a configured root history size does not exceed MaxRootHistorySize.
func ValidateRootHistorySize(size uint32) error {
	if size > MaxRootHistorySize {
		return fmt.Errorf("root history size must not exceed %d, got %d", MaxRootHistorySize, size)
	}
	return nil
}

// EncodeHex is a convenience function to encode byte slices as 0x prefixed hexadecimal strings.
func EncodeHex(bz []byte) string {
	return fmt.Sprintf("0x%s", hex.EncodeToString(bz))
}
