package app

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	"github.com/stretchr/testify/require"
)

type msgsTx struct {
	sdk.Tx
	msgs []sdk.Msg
}

func (tx msgsTx) GetMsgs() []sdk.Msg { return tx.msgs }

func TestValidateModuleQuerySafeRequests(t *testing.T) {
	addr := sdk.AccAddress("addr________________")
	querySafe := func(n int) sdk.Msg {
		requests := make([]*icahosttypes.QueryRequest, n)
		for i := range requests {
			requests[i] = &icahosttypes.QueryRequest{Path: "/cosmos.bank.v1beta1.Query/TotalSupply"}
		}
		return icahosttypes.NewMsgModuleQuerySafe(addr.String(), requests)
	}
	exec := func(msg sdk.Msg) sdk.Msg {
		m := authz.NewMsgExec(addr, []sdk.Msg{msg})
		return &m
	}
	proposal := func(msg sdk.Msg) sdk.Msg {
		m, err := govv1.NewMsgSubmitProposal([]sdk.Msg{msg}, nil, addr.String(), "", "title", "summary", false)
		require.NoError(t, err)
		return m
	}
	send := banktypes.NewMsgSend(addr, addr, nil)

	testCases := []struct {
		name    string
		msgs    []sdk.Msg
		wantErr bool
	}{
		{"no MsgModuleQuerySafe", []sdk.Msg{send}, false},
		{"no requests", []sdk.Msg{querySafe(0)}, false},
		{"one request", []sdk.Msg{querySafe(1)}, false},
		{"two requests", []sdk.Msg{send, querySafe(2)}, true},
		{"one request in MsgExec", []sdk.Msg{exec(querySafe(1))}, false},
		{"two requests in MsgExec", []sdk.Msg{exec(querySafe(2))}, true},
		{"one request in MsgSubmitProposal", []sdk.Msg{proposal(querySafe(1))}, false},
		{"two requests in MsgSubmitProposal", []sdk.Msg{proposal(querySafe(2))}, true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateModuleQuerySafeRequests(msgsTx{msgs: tc.msgs})
			if tc.wantErr {
				require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
