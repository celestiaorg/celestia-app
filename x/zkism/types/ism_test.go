package types_test

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v4/x/zkism/types"
	"github.com/stretchr/testify/require"
)

func TestVerify(t *testing.T) {
	var (
		trustedStateRoot = "4913ECE12489492945CEAA6150D99E29A9FFFFE32473E092084E3618C81246B1"
		vkeyHash         = "0x00183937c778e814051e7b0d5a035dc578804ec2a7418e5446a921504d458f5c"
	)

	groth16Vk, proofBz, inputsBz := readProofData(t)

	vkCommitmentHex := strings.TrimPrefix(string(vkeyHash), "0x")
	vkCommitment, err := hex.DecodeString(vkCommitmentHex)
	require.NoError(t, err)

	trustedRoot, err := hex.DecodeString(trustedStateRoot)
	require.NoError(t, err)

	// create an ism with a hardcoded initial trusted state
	ism := types.ZKExecutionISM{
		StateTransitionVerifierKey: groth16Vk,
		VerifierKeyCommitment:      vkCommitment,
		StateRoot:                  trustedRoot,
		Height:                     44,
	}

	metadata := encodeMetadata(t, proofBz, inputsBz)

	verified, err := ism.Verify(context.Background(), metadata, util.HyperlaneMessage{})
	require.NoError(t, err)
	require.True(t, verified)

	inputs := new(types.PublicInputs)
	err = inputs.Unmarshal(inputsBz)
	require.NoError(t, err)

	require.Equal(t, inputs.NewStateRoot[:], ism.StateRoot)
	require.Equal(t, inputs.NewHeight, ism.Height)
}

// encodeMetadata: [proofType][proofSize][proof][pubInputsSize][pubInputs]
// Note: Merkle proofs for state membership are omitted here
func encodeMetadata(t *testing.T, proofBz, pubInputs []byte) []byte {
	t.Helper()

	proofSize := make([]byte, 4)
	binary.BigEndian.PutUint32(proofSize, uint32(len(proofBz)))

	pubInputsSize := make([]byte, 4)
	binary.BigEndian.PutUint32(pubInputsSize, uint32(len(pubInputs)))

	var metadata []byte
	metadata = append(metadata, byte(types.ProofTypeSP1Groth16))
	metadata = append(metadata, proofSize...)
	metadata = append(metadata, proofBz...)
	metadata = append(metadata, pubInputsSize...)
	metadata = append(metadata, pubInputs...)

	return metadata
}

func readProofData(t *testing.T) ([]byte, []byte, []byte) {
	t.Helper()

	groth16Vk, err := os.ReadFile("../internal/testdata/groth16_vk.bin")
	require.NoError(t, err, "failed to read verifier key file")

	proofBz, err := os.ReadFile("../internal/testdata/proof.bin")
	require.NoError(t, err, "failed to read proof file")

	inputsBz, err := os.ReadFile("../internal/testdata/sp1_inputs.bin")
	require.NoError(t, err, "failed to read proof file")

	return groth16Vk, proofBz, inputsBz
}
