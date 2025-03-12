package app

import (
	"cosmossdk.io/errors"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/celestiaorg/celestia-app/v4/app/ante"
)

// GovParamFilters returns the params that require a hardfork to change, and
// cannot be changed via governance.
func (app *App) GovParamFilters() map[string]ante.ParamFilter {
	return map[string]ante.ParamFilter{
		sdk.MsgTypeURL((*banktypes.MsgUpdateParams)(nil)):      bankParamFilter,
		sdk.MsgTypeURL((*stakingtypes.MsgUpdateParams)(nil)):   stakingParamFilter,
		sdk.MsgTypeURL((*consensustypes.MsgUpdateParams)(nil)): consensusParamFilter,
	}
}

// bankParamFilter checks if the parameters in the MsgUpdateParams ensures that only valid changes are allowed.
func bankParamFilter(msg sdk.Msg) error {
	msgUpdateParams, ok := msg.(*banktypes.MsgUpdateParams)
	if !ok {
		return errors.Wrapf(sdkerrors.ErrInvalidType, "expected %T, got %T", (*banktypes.MsgUpdateParams)(nil), msg)
	}

	// ensure SendEnabled is not modified.
	//nolint:staticcheck
	if len(msgUpdateParams.Params.SendEnabled) > 0 || !msgUpdateParams.Params.DefaultSendEnabled {
		return errors.Wrapf(sdkerrors.ErrUnauthorized, "modification of SendEnabled is not allowed")
	}

	return nil
}

// consensusParamstakingParamFilterFilter checks if the parameters in the MsgUpdateParams ensures that only valid changes are allowed.
func stakingParamFilter(msg sdk.Msg) error {
	msgUpdateParams, ok := msg.(*stakingtypes.MsgUpdateParams)
	if !ok {
		return errors.Wrapf(sdkerrors.ErrInvalidType, "expected %T, got %T", (*stakingtypes.MsgUpdateParams)(nil), msg)
	}

	defaultParams := stakingtypes.DefaultParams()
	if msgUpdateParams.Params.BondDenom != defaultParams.BondDenom {
		return errors.Wrapf(sdkerrors.ErrUnauthorized, "invalid bond denom: expected %s, got %s", defaultParams.BondDenom, msgUpdateParams.Params.BondDenom)
	}

	if msgUpdateParams.Params.UnbondingTime != defaultParams.UnbondingTime {
		return errors.Wrapf(sdkerrors.ErrUnauthorized, "invalid unbonding time: expected %s, got %s", defaultParams.UnbondingTime, msgUpdateParams.Params.UnbondingTime)
	}

	return nil
}

// consensusParamFilter checks if the parameters in the MsgUpdateParams ensures that only valid changes are allowed.
func consensusParamFilter(msg sdk.Msg) error {
	msgUpdateParams, ok := msg.(*consensustypes.MsgUpdateParams)
	if !ok {
		return errors.Wrapf(sdkerrors.ErrInvalidType, "expected %T, got %T", (*consensustypes.MsgUpdateParams)(nil), msg)
	}

	validatorParams := coretypes.DefaultConsensusParams().ToProto().Validator
	updateParams, err := msgUpdateParams.ToProtoConsensusParams()
	if err != nil {
		return err
	}

	if !updateParams.Validator.Equal(validatorParams) {
		return errors.Wrapf(sdkerrors.ErrUnauthorized, "invalid validator parameters")
	}

	return nil
}
