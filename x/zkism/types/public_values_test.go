package types_test

import (
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/stretchr/testify/require"
)

func TestStateTransitionPublicValuesEncoding(t *testing.T) {
	expected := types.EvExecutionPublicValues{
		CelestiaHeaderHash: [32]byte{0x01},
		TrustedHeight:      123,
		TrustedStateRoot:   [32]byte{0xAA},
		NewHeight:          456,
		NewStateRoot:       [32]byte{0xBB},
		Namespace:          [29]byte{0xCC},
		PublicKey:          [32]byte{0xDD},
	}

	bz, err := expected.Marshal()
	require.NoError(t, err)
	require.NotEmpty(t, bz)

	var decoded types.EvExecutionPublicValues
	err = decoded.Unmarshal(bz)
	require.NoError(t, err)

	require.Equal(t, expected.CelestiaHeaderHash, decoded.CelestiaHeaderHash)
	require.Equal(t, expected.TrustedHeight, decoded.TrustedHeight)
	require.Equal(t, expected.TrustedStateRoot, decoded.TrustedStateRoot)
	require.Equal(t, expected.NewHeight, decoded.NewHeight)
	require.Equal(t, expected.NewStateRoot, decoded.NewStateRoot)
	require.Equal(t, expected.Namespace, decoded.Namespace)
	require.Equal(t, expected.PublicKey, decoded.PublicKey)
}

func TestStateTransitionPublicValuesTrailingData(t *testing.T) {
	pubInputs := types.EvExecutionPublicValues{
		CelestiaHeaderHash: [32]byte{0x01},
		TrustedHeight:      1,
		TrustedStateRoot:   [32]byte{0x02},
		NewHeight:          2,
		NewStateRoot:       [32]byte{0x03},
		Namespace:          [29]byte{0x04},
		PublicKey:          [32]byte{0x04},
	}

	bz, err := pubInputs.Marshal()
	require.NoError(t, err)

	bz = append(bz, 0xFF) // append trailing data to force error

	var decoded types.EvExecutionPublicValues
	err = decoded.Unmarshal(bz)
	require.Error(t, err)
	require.Contains(t, err.Error(), "trailing data")
}

func TestStateMembershipPublicValuesEncoding(t *testing.T) {
	messageIds := make([][32]byte, 0, 5)
	for i := range 5 {
		msg := util.HyperlaneMessage{
			Nonce: uint32(i),
		}

		messageIds = append(messageIds, msg.Id())
	}

	expected := types.EvHyperlanePublicValues{
		StateRoot:  [32]byte{0x01},
		MessageIds: messageIds,
	}

	bz, err := expected.Marshal()
	require.NoError(t, err)
	require.NotEmpty(t, bz)

	var decoded types.EvHyperlanePublicValues
	err = decoded.Unmarshal(bz)
	require.NoError(t, err)

	require.Equal(t, expected.StateRoot, decoded.StateRoot)
	require.Len(t, decoded.MessageIds, len(expected.MessageIds))
	require.Equal(t, expected.MessageIds, decoded.MessageIds)
}
