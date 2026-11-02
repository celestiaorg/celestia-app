package types_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
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
	expected := types.StateMembershipValues{
		StateRoot:         [32]byte{0x01},
		MerkleTreeAddress: [32]byte{0x02},
		TreeRoot:          [32]byte{0x03},
		Count:             math.MaxUint32,
	}

	bz, err := expected.Marshal()
	require.NoError(t, err)
	require.Len(t, bz, types.StateMembershipValuesBytes)

	var decoded types.StateMembershipValues
	err = decoded.Unmarshal(bz)
	require.NoError(t, err)
	require.Equal(t, expected, decoded)
}

func TestStateMembershipPublicValuesUnmarshalRejectsInvalidLength(t *testing.T) {
	// 72 and 104 are the lengths of legacy message id payloads (72 + 32 * n) with zero and one message id. The
	// single id payload used to collide with the previous u64 count layout; the decoder must never accept them.
	for _, n := range []int{0, 1, 32, 72, 104, types.StateMembershipValuesBytes - 1, types.StateMembershipValuesBytes + 1} {
		var v types.StateMembershipValues
		err := v.Unmarshal(make([]byte, n))
		require.Error(t, err, "expected error on %d-byte input", n)
	}
}

func TestStateMembershipPublicValuesUnmarshalRejectsUnsupportedVersion(t *testing.T) {
	valid, err := (&types.StateMembershipValues{Count: 1}).Marshal()
	require.NoError(t, err)

	for _, version := range []byte{0, types.StateMembershipValuesVersion + 1, 0xFF} {
		payload := bytes.Clone(valid)
		payload[0] = version

		var decoded types.StateMembershipValues
		err := decoded.Unmarshal(payload)
		require.Error(t, err, "expected error on version %d", version)
		require.Contains(t, err.Error(), "unsupported state membership values version")
	}
}
