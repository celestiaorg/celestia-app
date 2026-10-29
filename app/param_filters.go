package app

import (
	"slices"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/app/params"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
)

// GovParamFilters returns the params that require a hardfork to change, and
// cannot be changed via governance.
func (app *App) GovParamFilters() map[string]ante.ParamFilter {
	return map[string]ante.ParamFilter{
		sdk.MsgTypeURL((*banktypes.MsgUpdateParams)(nil)):      bankParamFilter,
		sdk.MsgTypeURL((*stakingtypes.MsgUpdateParams)(nil)):   stakingParamFilter,
		sdk.MsgTypeURL((*consensustypes.MsgUpdateParams)(nil)): consensusParamFilter,
		sdk.MsgTypeURL((*icahosttypes.MsgUpdateParams)(nil)):   icaHostParamFilter,
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

// stakingParamFilter ensures that the parameters in the MsgUpdateParams contain only valid changes.
func stakingParamFilter(msg sdk.Msg) error {
	msgUpdateParams, ok := msg.(*stakingtypes.MsgUpdateParams)
	if !ok {
		return errors.Wrapf(sdkerrors.ErrInvalidType, "expected %T, got %T", (*stakingtypes.MsgUpdateParams)(nil), msg)
	}

	if msgUpdateParams.Params.BondDenom != params.BondDenom {
		return errors.Wrapf(sdkerrors.ErrUnauthorized, "invalid bond denom: expected %s, got %s", params.BondDenom, msgUpdateParams.Params.BondDenom)
	}

	if msgUpdateParams.Params.UnbondingTime != appconsts.UnbondingTime {
		return errors.Wrapf(sdkerrors.ErrUnauthorized, "invalid unbonding time: expected %s, got %s", appconsts.UnbondingTime, msgUpdateParams.Params.UnbondingTime)
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

	evidenceParams := EvidenceParams()
	if !updateParams.Evidence.Equal(evidenceParams) {
		return errors.Wrapf(sdkerrors.ErrUnauthorized, "invalid evidence parameters")
	}

	return nil
}

// icaHostParamFilter only lets governance narrow the ICA host allowlist. The
// per-block counters assume the allowlist holds no fan-out or proof-work
// messages, so adding a type requires a hardfork. Like all proposal checks it
// runs at submission, so before the v11 upgrade height, check that no pending
// proposal would widen the allowlist or carry messages the v11 ante rejects.
func icaHostParamFilter(msg sdk.Msg) error {
	msgUpdateParams, ok := msg.(*icahosttypes.MsgUpdateParams)
	if !ok {
		return errors.Wrapf(sdkerrors.ErrInvalidType, "expected %T, got %T", (*icahosttypes.MsgUpdateParams)(nil), msg)
	}

	allowed := IcaAllowMessages()
	for _, typeURL := range msgUpdateParams.Params.AllowMessages {
		if !slices.Contains(allowed, typeURL) {
			return errors.Wrapf(sdkerrors.ErrUnauthorized, "ICA host allow message %q is not allowed", typeURL)
		}
	}

	return nil
}
