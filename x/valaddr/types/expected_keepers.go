package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// StakingKeeper defines the expected staking keeper interface
type StakingKeeper interface {
	// GetValidator returns a validator by operator address
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
	// GetValidatorByConsAddr returns a validator by consensus address. It
	// returns stakingtypes.ErrNoValidatorFound once the validator has been
	// removed from staking state.
	GetValidatorByConsAddr(ctx context.Context, consAddr sdk.ConsAddress) (stakingtypes.Validator, error)
	// GetBondedValidatorsByPower returns all bonded validators sorted by power
	GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error)
}
