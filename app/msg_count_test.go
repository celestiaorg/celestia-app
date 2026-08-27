package app

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestCountExecutableMsgs(t *testing.T) {
	send := func() sdk.Msg { return &banktypes.MsgSend{} }

	msgExecWith := func(n int) sdk.Msg {
		inner := make([]sdk.Msg, n)
		for i := range inner {
			inner[i] = send()
		}
		exec := authz.NewMsgExec(sdk.AccAddress{}, inner)
		return &exec
	}

	testCases := []struct {
		name     string
		msgs     []sdk.Msg
		expected int
	}{
		{
			name:     "plain messages count as one each",
			msgs:     []sdk.Msg{send(), send(), send()},
			expected: 3,
		},
		{
			name:     "MsgExec counts its inner messages",
			msgs:     []sdk.Msg{msgExecWith(99)},
			expected: 99,
		},
		{
			name:     "mix of plain and MsgExec",
			msgs:     []sdk.Msg{msgExecWith(2), send(), msgExecWith(3)},
			expected: 6,
		},
		{
			name:     "empty MsgExec counts as zero",
			msgs:     []sdk.Msg{msgExecWith(0)},
			expected: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, countExecutableMsgs(tc.msgs))
		})
	}
}
