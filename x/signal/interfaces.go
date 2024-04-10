package signal

import (
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

type StakingKeeper interface {
	GetLastValidatorPower(ctx sdk.Context, addr sdk.ValAddress) int64
	GetLastTotalPower(ctx sdk.Context) math.Int
	GetValidator(ctx sdk.Context, addr sdk.ValAddress) (validator stakingtypes.Validator, found bool)
}
