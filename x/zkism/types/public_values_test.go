package types_test

import (
	"bytes"
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/stretchr/testify/require"
)

func TestStateTransitionPublicValuesEncoding(t *testing.T) {
	expected := types.StateTransitionValues{
		State:    bytes.Repeat([]byte{0x01}, types.MinStateBytes),
		NewState: bytes.Repeat([]byte{0xFF}, types.MaxStateBytes),
	}

	bz, err := expected.Marshal()
	require.NoError(t, err)
	require.NotEmpty(t, bz)

	var decoded types.StateTransitionValues
	err = decoded.Unmarshal(bz)
	require.NoError(t, err)

	require.Equal(t, expected.State, decoded.State)
}

func TestStateTransitionPublicValuesUnmarshalFailure(t *testing.T) {
	expected := types.StateTransitionValues{
		State:    bytes.Repeat([]byte{0x01}, 2050),
		NewState: bytes.Repeat([]byte{0x01}, types.MinStateBytes),
	}

	bz, err := expected.Marshal()
	require.NoError(t, err)
	require.NotEmpty(t, bz)

	var decoded types.StateTransitionValues
	err = decoded.Unmarshal(bz)
	require.Error(t, err)

	expected = types.StateTransitionValues{
		State:    bytes.Repeat([]byte{0x01}, types.MaxStateBytes),
		NewState: bytes.Repeat([]byte{0x01}, 2050),
	}

	bz, err = expected.Marshal()
	require.NoError(t, err)
	require.NotEmpty(t, bz)

	err = decoded.Unmarshal(bz)
	require.Error(t, err)
}

func TestStateMembershipPublicValuesEncoding(t *testing.T) {
	messageIds := make([][32]byte, 0, 5)
	for i := range 5 {
		msg := util.HyperlaneMessage{
			Nonce: uint32(i),
		}

		messageIds = append(messageIds, msg.Id())
	}

	expected := types.StateMembershipValues{
		StateRoot:         [32]byte{0x01},
		MerkleTreeAddress: [32]byte{0x02},
		MessageIds:        messageIds,
	}

	bz, err := expected.Marshal()
	require.NoError(t, err)
	require.NotEmpty(t, bz)

	var decoded types.StateMembershipValues
	err = decoded.Unmarshal(bz)
	require.NoError(t, err)

	require.Equal(t, expected.StateRoot, decoded.StateRoot)
	require.Equal(t, expected.MerkleTreeAddress, decoded.MerkleTreeAddress)
	require.Len(t, decoded.MessageIds, len(expected.MessageIds))
	require.Equal(t, expected.MessageIds, decoded.MessageIds)
}
