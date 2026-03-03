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
	// GetBondedValidatorsByPower returns all bonded validators sorted by power
	GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error)
}
