package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v6/x/valaddr/types"
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

// AllFibreProviders queries fibre provider information for all validators that have a host defined
func (k Keeper) AllFibreProviders(goCtx context.Context, req *types.QueryAllFibreProvidersRequest) (*types.QueryAllFibreProvidersResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidValidator, "empty request")
	}

	var providers []types.FibreProvider
	err := k.IterateFibreProviderInfo(goCtx, func(consAddr sdk.ConsAddress, info types.FibreProviderInfo) bool {
		providers = append(providers, types.FibreProvider{
			ValidatorConsensusAddress: consAddr.String(),
			Info:                      info,
		})
		return false
	})
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to iterate fibre provider info")
	}

	return &types.QueryAllFibreProvidersResponse{
		Providers: providers,
	}, nil
}
