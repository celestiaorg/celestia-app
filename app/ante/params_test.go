package ante_test

import (
	"errors"
	"fmt"
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app/ante"
)

func TestGovProposalDecorator_AnteHandle(t *testing.T) {
	testCases := []struct {
		name          string
		msgs          []sdk.Msg
		paramFilters  map[string]ante.ParamFilter
		expectedError error
	}{
		{
			name: "valid MsgSubmitProposal with allowed msg",
			msgs: []sdk.Msg{
				createMsgSubmitProposal(&banktypes.MsgUpdateParams{}),
			},
			paramFilters: map[string]ante.ParamFilter{
				sdk.MsgTypeURL(&banktypes.MsgUpdateParams{}): func(sdk.Msg) error {
					return nil
				},
			},
			expectedError: nil,
		},
		{
			name: "invalid MsgSubmitProposal with disallowed msg",
			msgs: []sdk.Msg{
				createMsgSubmitProposal(&banktypes.MsgUpdateParams{}),
			},
			paramFilters: map[string]ante.ParamFilter{
				sdk.MsgTypeURL(&banktypes.MsgUpdateParams{}): func(sdk.Msg) error {
					return fmt.Errorf("unauthorized message")
				},
			},
			expectedError: fmt.Errorf("unauthorized message"),
		},
		{
			name: "invalid MsgSubmitProposal with disallowed params",
			msgs: []sdk.Msg{
				createMsgSubmitProposal(&authz.MsgGrant{}),
			},
			paramFilters: map[string]ante.ParamFilter{
				sdk.MsgTypeURL(&authz.MsgGrant{}): func(sdk.Msg) error {
					return errors.New("unauthorized message in proposal")
				},
			},
			expectedError: errors.New("unauthorized message in proposal"),
		},
		{
			name: "valid Msg outside of MsgSubmitProposal or Authz",
			msgs: []sdk.Msg{
				&banktypes.MsgSend{},
			},
			paramFilters: map[string]ante.ParamFilter{
				sdk.MsgTypeURL(&banktypes.MsgSend{}): func(sdk.Msg) error {
					return errors.New("unauthorized message")
				},
			},
			expectedError: nil,
		},
		{
			name: "empty MsgSubmitProposal",
			msgs: []sdk.Msg{
				createMsgSubmitProposal(),
			},
			paramFilters:  map[string]ante.ParamFilter{},
			expectedError: errors.New("must include at least one message: invalid request"),
		},
		{
			name: "invalid generic message",
			msgs: []sdk.Msg{
				createMsgSubmitProposal(&authz.MsgRevoke{}),
			},
			paramFilters: map[string]ante.ParamFilter{
				sdk.MsgTypeURL(&authz.MsgRevoke{}): func(sdk.Msg) error {
					return errors.New("unauthorized message")
				},
			},
			expectedError: errors.New("unauthorized message"),
		},
		{
			name: "MsgSubmitProposal with mixed messages (valid and invalid)",
			msgs: []sdk.Msg{
				createMsgSubmitProposal(&banktypes.MsgSend{}, &authz.MsgGrant{}),
			},
			paramFilters: map[string]ante.ParamFilter{
				sdk.MsgTypeURL(&banktypes.MsgSend{}): func(sdk.Msg) error {
					return nil
				},
				sdk.MsgTypeURL(&authz.MsgGrant{}): func(sdk.Msg) error {
					return errors.New("unauthorized message in proposal")
				},
			},
			expectedError: errors.New("unauthorized message in proposal"),
		},
		{
			name: "empty AuthZ",
			msgs: []sdk.Msg{
				createAuthzMsgExec(),
			},
			paramFilters:  map[string]ante.ParamFilter{},
			expectedError: errors.New("must include at least one message: invalid request"),
		},
		{
			name: "nested governance proposal",
			msgs: []sdk.Msg{
				createMsgSubmitProposal(createMsgSubmitProposal(&banktypes.MsgUpdateParams{})),
			},
			paramFilters: map[string]ante.ParamFilter{
				sdk.MsgTypeURL(&banktypes.MsgUpdateParams{}): func(sdk.Msg) error {
					return fmt.Errorf("unauthorized message")
				},
			},
			expectedError: fmt.Errorf("unauthorized message"),
		},
		{
			name: "nested authz proposal",
			msgs: []sdk.Msg{
				createAuthzMsgExec(createAuthzMsgExec(&banktypes.MsgUpdateParams{})),
			},
			paramFilters: map[string]ante.ParamFilter{
				sdk.MsgTypeURL(&banktypes.MsgUpdateParams{}): func(sdk.Msg) error {
					return fmt.Errorf("unauthorized message")
				},
			},
			expectedError: fmt.Errorf("unauthorized message"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			anteHandler := ante.NewParamFilterDecorator(tc.paramFilters)
			_, err := anteHandler.AnteHandle(sdk.Context{}, mockTx(tc.msgs), false, nextAnteHandler)
			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func mockTx(msgs []sdk.Msg) sdk.Tx {
	return &mockTxImplementation{msgs: msgs}
}

type mockTxImplementation struct {
	sdk.Tx
	msgs []sdk.Msg
}

func (m *mockTxImplementation) GetMsgs() []sdk.Msg {
	return m.msgs
}

func nextAnteHandler(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}

// createAuthzMsgExec constructs an instance of authz.MsgExec containing the provided messages.
func createAuthzMsgExec(msgs ...sdk.Msg) *authz.MsgExec {
	anys := make([]*codectypes.Any, len(msgs))

	for i, msg := range msgs {
		anyMsg, err := codectypes.NewAnyWithValue(msg)
		if err != nil {
			panic(err)
		}
		anys[i] = anyMsg
	}

	return &authz.MsgExec{
		Msgs: anys,
	}
}

// createMsgSubmitProposal constructs an instance of govv1.MsgSubmitProposal containing the provided messages.
func createMsgSubmitProposal(msgs ...sdk.Msg) *govv1.MsgSubmitProposal {
	anys := make([]*codectypes.Any, len(msgs))
	for i, msg := range msgs {
		anyMsg, err := codectypes.NewAnyWithValue(msg)
		if err != nil {
			panic(err)
		}
		anys[i] = anyMsg
	}

	return &govv1.MsgSubmitProposal{
		Messages: anys,
	}
}
