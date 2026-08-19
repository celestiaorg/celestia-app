package app

import (
	"context"
	"errors"

	evidencetypes "cosmossdk.io/x/evidence/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// evidenceStakingKeeper wraps the staking keeper used by the evidence module so
// that equivocation evidence naming a validator whose record no longer exists
// is ignored instead of halting the chain.
//
// The evidence handler treats a non-nil error from ValidatorByConsAddr as fatal
// and returns it, which propagates out of FinalizeBlock with nothing to recover
// it. ValidatorByConsAddr returns ErrNoValidatorFound once the validator's
// consensus-address index entry has been deleted, so the handler's own
// defensive "validator == nil" branch is never reached. Translating that error
// into (nil, nil) restores the intended defensive behavior.
type evidenceStakingKeeper struct {
	evidencetypes.StakingKeeper
}

func (k evidenceStakingKeeper) ValidatorByConsAddr(ctx context.Context, consAddr sdk.ConsAddress) (stakingtypes.ValidatorI, error) {
	validator, err := k.StakingKeeper.ValidatorByConsAddr(ctx, consAddr)
	if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
		return nil, nil
	}
	return validator, err
}
