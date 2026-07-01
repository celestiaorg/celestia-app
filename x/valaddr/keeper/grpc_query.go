package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v10/x/valaddr/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ types.QueryServer = Keeper{}

// FibreProviderInfo queries the fibre provider information for a specific validator
func (k Keeper) FibreProviderInfo(goCtx context.Context, req *types.QueryFibreProviderInfoRequest) (*types.QueryFibreProviderInfoResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidValidator, "empty request")
	}

	consAddr, err := sdk.ConsAddressFromBech32(req.ValidatorConsensusAddress)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrInvalidValidator, "invalid consensus address: %v", err)
	}

	info, found := k.GetFibreProviderInfo(goCtx, consAddr)

	return &types.QueryFibreProviderInfoResponse{
		Info:  &info,
		Found: found,
	}, nil
}

// AllBondedFibreProviders returns the fibre provider info of every currently
// bonded validator that has registered a host. Providers whose validator has
// left the active set (unbonded, or jailed and no longer bonded) are omitted so
// callers never dial a stale host; such entries are also garbage-collected by
// the EndBlocker sweep (see keeper.RemoveStaleFibreProviders).
func (k Keeper) AllBondedFibreProviders(goCtx context.Context, req *types.QueryAllBondedFibreProvidersRequest) (*types.QueryAllBondedFibreProvidersResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidValidator, "empty request")
	}

	var providers []types.FibreProvider
	err := k.IterateFibreProviderInfo(goCtx, func(consAddr sdk.ConsAddress, info types.FibreProviderInfo) bool {
		validator, err := k.stakingKeeper.GetValidatorByConsAddr(goCtx, consAddr)
		if err != nil || !validator.IsBonded() {
			return false
		}
		providers = append(providers, types.FibreProvider{
			ValidatorConsensusAddress: consAddr.String(),
			Info:                      info,
		})
		return false
	})
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to iterate fibre provider info")
	}

	return &types.QueryAllBondedFibreProvidersResponse{
		Providers: providers,
	}, nil
}
