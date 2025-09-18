package types_test

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/stretchr/testify/require"
)

func TestVerify(t *testing.T) {
	t.Skip("TODO: refactor func implementation and test")

	var (
		celHeight        = 30
		trustedStateRoot = "af50a407e7a9fcba29c46ad31e7690bae4e951e3810e5b898eda29d3d3e92dbe"
		vkeyHash         = "0x00acd6f9c9d0074611353a1e0c94751d3c49beef64ebc3ee82f0ddeadaf242ef"
		namespaceHex     = "00000000000000000000000000000000000000a8045f161bf468bf4d44"
		publicKeyHex     = "c87f6c4cdd4c8ac26cb6a06909e5e252b73043fdf85232c18ae92b9922b65507"
	)

	groth16Vk, proofBz, inputsBz := readProofData(t)

	vkCommitmentHex := strings.TrimPrefix(vkeyHash, "0x")
	vkCommitment, err := hex.DecodeString(vkCommitmentHex)
	require.NoError(t, err)

	trustedRoot, err := hex.DecodeString(trustedStateRoot)
	require.NoError(t, err)

	namespace, err := hex.DecodeString(namespaceHex)
	require.NoError(t, err)

	pubKey, err := hex.DecodeString(publicKeyHex)
	require.NoError(t, err)

	// create an ism with a hardcoded initial trusted state
	ism := types.ZKExecutionISM{
		Groth16Vkey:         groth16Vk,
		StateTransitionVkey: vkCommitment,
		StateMembershipVkey: []byte("todo"),
		StateRoot:           trustedRoot,
		Height:              97,
		Namespace:           namespace,
		SequencerPublicKey:  pubKey,
	}

	metadata := encodeMetadata(t, uint64(celHeight), proofBz, inputsBz)

	verified, err := ism.Verify(context.Background(), metadata, util.HyperlaneMessage{})
	require.NoError(t, err)
	require.True(t, verified)
}

// encodeMetadata: [proofType][proofSize][proof][pubInputsSize][pubInputs]
// Note: Merkle proofs for state membership are omitted here
func encodeMetadata(t *testing.T, height uint64, proofBz, pubInputs []byte) []byte {
	t.Helper()

	heightBz := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBz, height)

	proofSize := make([]byte, 4)
	binary.BigEndian.PutUint32(proofSize, uint32(len(proofBz)))

	pubInputsSize := make([]byte, 4)
	binary.BigEndian.PutUint32(pubInputsSize, uint32(len(pubInputs)))

	var metadata []byte
	metadata = append(metadata, byte(types.ProofTypeSP1Groth16))
	metadata = append(metadata, heightBz...)
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
