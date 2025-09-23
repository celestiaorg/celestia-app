package types_test

import (
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/stretchr/testify/require"
)

func TestNewGroth16Verifier(t *testing.T) {
	groth16Vk := readGroth16Vkey(t)

	tests := []struct {
		name     string
		vk       []byte
		expError error
	}{
		{
			name:     "valid verifier key",
			vk:       groth16Vk,
			expError: nil,
		},
		{
			name:     "empty verifier key",
			vk:       []byte{},
			expError: types.ErrInvalidVerifyingKey,
		},
		{
			name:     "truncated verifier key",
			vk:       groth16Vk[:16],
			expError: types.ErrInvalidVerifyingKey,
		},
		{
			name:     "random garbage",
			vk:       []byte{0x01, 0x02, 0x03, 0x04},
			expError: types.ErrInvalidVerifyingKey,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			verifier, err := types.NewSP1Groth16Verifier(tc.vk)

			if tc.expError != nil {
				require.Error(t, err, "expected error but got none")
				require.ErrorIs(t, err, tc.expError, "unexpected error")
				require.Nil(t, verifier)
			} else {
				require.NoError(t, err, "unexpected error constructing verifier")
				require.NotNil(t, verifier)
				require.NotEmpty(t, verifier)
			}
		})
	}
}

func TestVerifyProof(t *testing.T) {
	groth16Vk := readGroth16Vkey(t)
	proofBz, valuesBz := readProofAndValues(t)

	programVkHex := "0x00acd6f9c9d0074611353a1e0c94751d3c49beef64ebc3ee82f0ddeadaf242ef"
	programVk, err := hex.DecodeString(strings.TrimPrefix(programVkHex, "0x"))
	require.NoError(t, err)

	verifier, err := types.NewSP1Groth16Verifier(groth16Vk)
	require.NoError(t, err)

	tests := []struct {
		name      string
		proofBz   []byte
		programVk []byte
		valuesBz  []byte
		expError  error
	}{
		{
			name:      "valid proof",
			proofBz:   proofBz,
			programVk: programVk,
			valuesBz:  valuesBz,
			expError:  nil,
		},
		{
			name:      "invalid proof length",
			proofBz:   proofBz[:10],
			programVk: programVk,
			valuesBz:  valuesBz,
			expError:  types.ErrInvalidProofLength,
		},
		{
			name: "invalid prefix",
			proofBz: func() []byte {
				corrupt := make([]byte, len(proofBz))
				copy(corrupt, proofBz)

				corrupt[0] ^= 0xFF // flip first byte of prefix
				return corrupt
			}(),
			programVk: programVk,
			valuesBz:  valuesBz,
			expError:  types.ErrInvalidProofPrefix,
		},
		{
			name:      "nil program key",
			proofBz:   proofBz,
			programVk: nil,
			valuesBz:  valuesBz,
			expError:  types.ErrInvalidProof,
		},
		{
			name:      "corrupted values",
			proofBz:   proofBz,
			programVk: programVk,
			valuesBz:  []byte{0x01, 0x02, 0x03, 0x04},
			expError:  types.ErrInvalidProof,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := verifier.VerifyProof(tc.proofBz, tc.programVk, tc.valuesBz)
			if tc.expError != nil {
				require.Error(t, err, "expected error but got none")
				require.ErrorIs(t, err, tc.expError, "unexpected error")
			} else {
				require.NoError(t, err, "expected no error but got one")
			}
		})
	}
}

func readGroth16Vkey(t *testing.T) []byte {
	t.Helper()

	groth16Vkey, err := os.ReadFile("../internal/testdata/groth16_vk.bin")
	require.NoError(t, err, "failed to read verifier key file")

	return groth16Vkey
}

func readProofAndValues(t *testing.T) ([]byte, []byte) {
	t.Helper()

	proofBz, err := os.ReadFile("../internal/testdata/state_transition/proof.bin")
	require.NoError(t, err, "failed to read proof file")

	inputsBz, err := os.ReadFile("../internal/testdata/state_transition/public_values.bin")
	require.NoError(t, err, "failed to read proof file")

	return proofBz, inputsBz
}
