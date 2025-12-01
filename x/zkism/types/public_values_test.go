package types_test

import (
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/stretchr/testify/require"
)

func TestStateTransitionPublicValuesEncoding(t *testing.T) {
	expected := types.StateTransitionInputs{
		State:    []byte{0x01, 0x02, 0x03, 0x04, 0x05},
		NewState: []byte{0x06, 0x07, 0x08, 0x09, 0x0A},
	}

	bz, err := expected.Marshal()
	require.NoError(t, err)
	require.NotEmpty(t, bz)

	var decoded types.StateTransitionInputs
	err = decoded.Unmarshal(bz)
	require.NoError(t, err)

	require.Equal(t, expected.State, decoded.State)
}

func TestStateMembershipPublicValuesEncoding(t *testing.T) {
	messageIds := make([][32]byte, 0, 5)
	for i := range 5 {
		msg := util.HyperlaneMessage{
			Nonce: uint32(i),
		}

		messageIds = append(messageIds, msg.Id())
	}

	expected := types.StateMembershipInputs{
		StateRoot:  [32]byte{0x01},
		MessageIds: messageIds,
	}

	bz, err := expected.Marshal()
	require.NoError(t, err)
	require.NotEmpty(t, bz)

	var decoded types.StateMembershipInputs
	err = decoded.Unmarshal(bz)
	require.NoError(t, err)

	require.Equal(t, expected.StateRoot, decoded.StateRoot)
	require.Len(t, decoded.MessageIds, len(expected.MessageIds))
	require.Equal(t, expected.MessageIds, decoded.MessageIds)
}
