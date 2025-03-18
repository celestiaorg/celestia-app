package app

import (
	"testing"
	"time"

	"cosmossdk.io/errors"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app/params"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

// TestGovParamFilters tests the functionality of retrieving governance parameter filters from the app.
// It ensures that the filters list is properly populated and contains the expected message types.
func TestGovParamFilters(t *testing.T) {
	app := &App{}
	filters := app.GovParamFilters()

	require.NotEmpty(t, filters)
	// ensure all keys are present in the map
	require.Contains(t, filters, sdk.MsgTypeURL((*banktypes.MsgUpdateParams)(nil)))
	require.Contains(t, filters, sdk.MsgTypeURL((*stakingtypes.MsgUpdateParams)(nil)))
	require.Contains(t, filters, sdk.MsgTypeURL((*consensustypes.MsgUpdateParams)(nil)))
}

// TestBankParamFilter tests the filtering logic for banktypes.MsgUpdateParams.
// It ensures that changes to parameters are correctly authorized, invalid message types
// return proper errors, and empty or default parameters are allowed without issues.
func TestBankParamFilter(t *testing.T) {
	tests := []struct {
		name        string
		params      sdk.Msg
		expectedErr error
	}{
		{
			name: "valid case: no SendEnabled modifications",
			params: &banktypes.MsgUpdateParams{
				Params: banktypes.Params{
					SendEnabled:        nil,
					DefaultSendEnabled: true,
				},
			},
			expectedErr: nil,
		},
		{
			name: "invalid case: SendEnabled modifications",
			params: &banktypes.MsgUpdateParams{
				Params: banktypes.Params{
					SendEnabled:        []*banktypes.SendEnabled{{Denom: "atom", Enabled: true}},
					DefaultSendEnabled: true,
				},
			},
			expectedErr: sdkerrors.ErrUnauthorized,
		},
		{
			name:        "invalid type passed to bankParamFilter",
			params:      &stakingtypes.MsgUpdateParams{},
			expectedErr: sdkerrors.ErrInvalidType,
		},
		{
			name: "success case: empty Params (allowed by default)",
			params: &banktypes.MsgUpdateParams{
				Params: banktypes.Params{
					SendEnabled:        []*banktypes.SendEnabled{},
					DefaultSendEnabled: true,
				},
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bankParamFilter(tt.params)
			if tt.expectedErr == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.True(t, errors.IsOf(err, tt.expectedErr))
			}
		})
	}
}

// TestStakingParamFilter tests the filtering logic for staking parameter update messages.
// It ensures that valid staking parameters or defaults are allowed, while invalid or unauthorized
// changes, as well as incorrect message types, return appropriate errors.
func TestStakingParamFilter(t *testing.T) {
	tests := []struct {
		name        string
		params      sdk.Msg
		expectedErr error
	}{
		{
			name: "valid case: default params",
			params: &stakingtypes.MsgUpdateParams{
				Params: stakingtypes.Params{
					BondDenom:     params.BondDenom,
					UnbondingTime: appconsts.DefaultUnbondingTime,
				},
			},
			expectedErr: nil,
		},
		{
			name: "invalid case: invalid bond denom",
			params: &stakingtypes.MsgUpdateParams{
				Params: stakingtypes.Params{
					BondDenom: "invalid",
				},
			},
			expectedErr: sdkerrors.ErrUnauthorized,
		},
		{
			name: "invalid case: invalid unbonding time",
			params: &stakingtypes.MsgUpdateParams{
				Params: stakingtypes.Params{
					BondDenom:     params.BondDenom,
					UnbondingTime: time.Hour * 48, // Different from default
				},
			},
			expectedErr: sdkerrors.ErrUnauthorized,
		},
		{
			name:        "invalid case: invalid type",
			params:      &banktypes.MsgUpdateParams{},
			expectedErr: sdkerrors.ErrInvalidType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := stakingParamFilter(tt.params)
			if tt.expectedErr == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.True(t, errors.IsOf(err, tt.expectedErr))
			}
		})
	}
}

// TestConsensusParamFilter tests filtering logic for consensus parameter update messages.
// It ensures that messages with default or acceptable parameters are allowed, while
// messages with unauthorized changes or invalid message types return an error.
func TestConsensusParamFilter(t *testing.T) {
	tests := []struct {
		name        string
		msg         sdk.Msg
		expectedErr error
	}{
		{
			name: "valid case: default params",
			msg: &consensustypes.MsgUpdateParams{
				Authority: "authority",
				Block:     coretypes.DefaultConsensusParams().ToProto().Block,
				Evidence:  coretypes.DefaultConsensusParams().ToProto().Evidence,
				Validator: coretypes.DefaultConsensusParams().ToProto().Validator,
				Abci:      coretypes.DefaultConsensusParams().ToProto().Abci,
			},
			expectedErr: nil,
		},
		{
			name: "valid case: modified block params",
			msg: &consensustypes.MsgUpdateParams{
				Authority: "authority",
				Block: &tmproto.BlockParams{
					MaxGas:   coretypes.DefaultConsensusParams().Block.MaxGas + 5000000, // modified value
					MaxBytes: coretypes.DefaultConsensusParams().Block.MaxBytes,
				},
				Evidence:  coretypes.DefaultConsensusParams().ToProto().Evidence,
				Validator: coretypes.DefaultConsensusParams().ToProto().Validator,
				Abci:      coretypes.DefaultConsensusParams().ToProto().Abci,
			},
			expectedErr: nil,
		},
		{
			name: "invalid case: non-default validator params",
			msg: &consensustypes.MsgUpdateParams{
				Authority: "authority",
				Block:     coretypes.DefaultConsensusParams().ToProto().Block,
				Evidence:  coretypes.DefaultConsensusParams().ToProto().Evidence,
				Validator: &tmproto.ValidatorParams{PubKeyTypes: []string{"invalid-type"}}, // Non-default value
				Abci:      coretypes.DefaultConsensusParams().ToProto().Abci,
			},
			expectedErr: sdkerrors.ErrUnauthorized,
		},
		{
			name:        "invalid case: incorrect message type",
			msg:         &banktypes.MsgUpdateParams{},
			expectedErr: sdkerrors.ErrInvalidType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := consensusParamFilter(tt.msg)
			if tt.expectedErr == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.True(t, errors.IsOf(err, tt.expectedErr))
			}
		})
	}
}
