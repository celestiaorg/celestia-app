package types_test

import (
	"encoding/binary"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/x/zkism/types"
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

	t.Run("inflated CommitmentKeys length is rejected", func(t *testing.T) {
		malicious := make([]byte, types.Groth16VkeySize)
		copy(malicious, validVK)
		// Set CommitmentKeys length at offset 388 to 0xFFFFFFFF.
		// This passes the size and G1.K checks but would cause gnark to
		// call make([]pedersen.VerifyingKey, 0xFFFFFFFF) and OOM.
		binary.BigEndian.PutUint32(malicious[388:392], 0xFFFFFFFF)
		err := types.ValidateGroth16Vkey(malicious)
		assert.ErrorContains(t, err, "CommitmentKeys length must be 0")
	})

	t.Run("inflated PublicAndCommitmentCommitted length is rejected", func(t *testing.T) {
		malicious := make([]byte, types.Groth16VkeySize)
		copy(malicious, validVK)
		// Set PublicAndCommitmentCommitted length at offset 392 to 0xFFFFFFFF.
		binary.BigEndian.PutUint32(malicious[392:396], 0xFFFFFFFF)
		err := types.ValidateGroth16Vkey(malicious)
		assert.ErrorContains(t, err, "PublicAndCommitmentCommitted length must be 0")
	})

	t.Run("non-zero CommitmentKeys length is rejected even if small", func(t *testing.T) {
		malicious := make([]byte, types.Groth16VkeySize)
		copy(malicious, validVK)
		binary.BigEndian.PutUint32(malicious[388:392], 1)
		err := types.ValidateGroth16Vkey(malicious)
		assert.ErrorContains(t, err, "CommitmentKeys length must be 0, got 1")
	})

	t.Run("non-zero PublicAndCommitmentCommitted length is rejected even if small", func(t *testing.T) {
		malicious := make([]byte, types.Groth16VkeySize)
		copy(malicious, validVK)
		binary.BigEndian.PutUint32(malicious[392:396], 1)
		err := types.ValidateGroth16Vkey(malicious)
		assert.ErrorContains(t, err, "PublicAndCommitmentCommitted length must be 0, got 1")
	})
}
