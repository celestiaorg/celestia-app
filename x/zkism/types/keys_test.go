package types_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateGroth16Vkey(t *testing.T) {
	validVK := readGroth16Vkey(t)

	t.Run("valid vkey passes", func(t *testing.T) {
		err := types.ValidateGroth16Vkey(validVK)
		require.NoError(t, err)
	})

	t.Run("wrong total size is rejected", func(t *testing.T) {
		err := types.ValidateGroth16Vkey(validVK[:100])
		assert.ErrorContains(t, err, "groth16 vkey must be exactly")
	})

	t.Run("inflated G1.K length is rejected", func(t *testing.T) {
		malicious := make([]byte, types.Groth16VkeySize)
		copy(malicious, validVK)
		binary.BigEndian.PutUint32(malicious[288:292], 0xFFFFFFFF)
		err := types.ValidateGroth16Vkey(malicious)
		assert.ErrorContains(t, err, "G1.K length must be")
	})

	t.Run("inflated PublicAndCommitmentCommitted length is rejected", func(t *testing.T) {
		malicious := make([]byte, types.Groth16VkeySize)
		copy(malicious, validVK)
		// Set PublicAndCommitmentCommitted length at offset 484 to 0xFFFFFFFF.
		// This passes the size and G1.K checks but would make gnark allocate
		// 0xFFFFFFFF slices and OOM.
		binary.BigEndian.PutUint32(malicious[484:488], 0xFFFFFFFF)
		err := types.ValidateGroth16Vkey(malicious)
		assert.ErrorContains(t, err, "PublicAndCommitmentCommitted length must be 0")
	})

	t.Run("inflated CommitmentKeys length is rejected", func(t *testing.T) {
		malicious := make([]byte, types.Groth16VkeySize)
		copy(malicious, validVK)
		// Set CommitmentKeys length at offset 488 to 0xFFFFFFFF, which would make
		// gnark allocate 0xFFFFFFFF pedersen verifying keys.
		binary.BigEndian.PutUint32(malicious[488:492], 0xFFFFFFFF)
		err := types.ValidateGroth16Vkey(malicious)
		assert.ErrorContains(t, err, "CommitmentKeys length must be 0")
	})

	t.Run("non-zero PublicAndCommitmentCommitted length is rejected even if small", func(t *testing.T) {
		malicious := make([]byte, types.Groth16VkeySize)
		copy(malicious, validVK)
		binary.BigEndian.PutUint32(malicious[484:488], 1)
		err := types.ValidateGroth16Vkey(malicious)
		assert.ErrorContains(t, err, "PublicAndCommitmentCommitted length must be 0, got 1")
	})

	t.Run("non-zero CommitmentKeys length is rejected even if small", func(t *testing.T) {
		malicious := make([]byte, types.Groth16VkeySize)
		copy(malicious, validVK)
		binary.BigEndian.PutUint32(malicious[488:492], 1)
		err := types.ValidateGroth16Vkey(malicious)
		assert.ErrorContains(t, err, "CommitmentKeys length must be 0, got 1")
	})

	// Clearing the compression-flag bits of any leading curve point's first byte makes gnark
	// read it as an uncompressed point, shifting its length-prefix reads past the offsets the
	// validator checks (CELESTIA-269). Every leading point must be rejected.
	for _, offset := range []int{0, 32, 64, 128, 192, 224} {
		t.Run(fmt.Sprintf("uncompressed leading curve point at offset %d is rejected", offset), func(t *testing.T) {
			malicious := make([]byte, types.Groth16VkeySize)
			copy(malicious, validVK)
			malicious[offset] &= 0x3f
			err := types.ValidateGroth16Vkey(malicious)
			assert.ErrorContains(t, err, "must be compressed")
		})
	}
}

// readLegacyGroth16Vkey returns the SP1 v5 verifying key of the ISMs created on
// mocha before the SP1 v6 verifier.
func readLegacyGroth16Vkey(t *testing.T) []byte {
	t.Helper()
	vk, err := os.ReadFile("../internal/testdata/groth16_sp1v5_test_key.bin")
	require.NoError(t, err)
	require.Len(t, vk, 396)
	return vk
}

func TestValidateGenesisGroth16Vkey(t *testing.T) {
	validVK := readGroth16Vkey(t)
	legacyVK := readLegacyGroth16Vkey(t)

	require.NoError(t, types.ValidateGenesisGroth16Vkey(validVK))
	require.NoError(t, types.ValidateGenesisGroth16Vkey(legacyVK))
	// Messages keep accepting only the SP1 v6 layout.
	require.ErrorContains(t, types.ValidateGroth16Vkey(legacyVK), "must be exactly 492 bytes, got 396")

	// A legacy key gets the same checks as an SP1 v6 one, at its own offsets:
	// G1.K length at 288, PublicAndCommitmentCommitted length at 388, CommitmentKeys length at 392.
	malformed := map[string]func(vk []byte){
		"inflated G1.K length":                         func(vk []byte) { binary.BigEndian.PutUint32(vk[288:292], 0xFFFFFFFF) },
		"SP1 v6 G1.K length":                           func(vk []byte) { binary.BigEndian.PutUint32(vk[288:292], 6) },
		"inflated PublicAndCommitmentCommitted length": func(vk []byte) { binary.BigEndian.PutUint32(vk[388:392], 0xFFFFFFFF) },
		"non-zero PublicAndCommitmentCommitted length": func(vk []byte) { binary.BigEndian.PutUint32(vk[388:392], 1) },
		"inflated CommitmentKeys":                      func(vk []byte) { binary.BigEndian.PutUint32(vk[392:396], 0xFFFFFFFF) },
	}
	for _, offset := range []int{0, 32, 64, 128, 192, 224} {
		malformed[fmt.Sprintf("uncompressed point at offset %d", offset)] = func(vk []byte) { vk[offset] &= 0x3f }
	}
	for name, corrupt := range malformed {
		t.Run(name, func(t *testing.T) {
			vk := bytes.Clone(legacyVK)
			corrupt(vk)
			require.Error(t, types.ValidateGenesisGroth16Vkey(vk))
		})
	}

	// Sizes of neither layout are rejected, for example 4 G1.K elements.
	require.ErrorContains(t, types.ValidateGenesisGroth16Vkey(make([]byte, 428)), "must be exactly 492 bytes, got 428")
}
