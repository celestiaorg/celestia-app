package app

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
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

	moduleQuerySafeWith := func(n int) sdk.Msg {
		requests := make([]*icahosttypes.QueryRequest, n)
		for i := range requests {
			requests[i] = &icahosttypes.QueryRequest{Path: "/cosmos.bank.v1beta1.Query/TotalSupply"}
		}
		return icahosttypes.NewMsgModuleQuerySafe("", requests)
	}

	msgExecWithMsgs := func(msgs ...sdk.Msg) sdk.Msg {
		exec := authz.NewMsgExec(sdk.AccAddress{}, msgs)
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
		{
			name:     "MsgModuleQuerySafe counts its queries",
			msgs:     []sdk.Msg{moduleQuerySafeWith(5)},
			expected: 5,
		},
		{
			name:     "MsgModuleQuerySafe inside MsgExec counts its queries",
			msgs:     []sdk.Msg{msgExecWithMsgs(moduleQuerySafeWith(5))},
			expected: 5,
		},
		{
			name:     "MsgModuleQuerySafe with no queries still counts as one",
			msgs:     []sdk.Msg{moduleQuerySafeWith(0)},
			expected: 1,
		},
		{
			name:     "mix of MsgModuleQuerySafe and plain messages",
			msgs:     []sdk.Msg{moduleQuerySafeWith(3), send(), msgExecWithMsgs(moduleQuerySafeWith(2), send())},
			expected: 7,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, countExecutableMsgs(tc.msgs))
		})
	}
}
