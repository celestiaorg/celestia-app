package app

import (
	"context"
	"errors"

	evidencetypes "cosmossdk.io/x/evidence/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// evidenceStakingKeeper wraps the staking keeper so that equivocation evidence
// naming a deleted validator is ignored instead of halting the chain: it maps
// ErrNoValidatorFound to (nil, nil), which the evidence handler skips instead
// of returning an error from FinalizeBlock.
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
