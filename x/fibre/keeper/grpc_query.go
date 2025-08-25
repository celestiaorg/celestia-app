package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	"cosmossdk.io/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ types.QueryServer = Keeper{}

// FibreProviderInfo implements the FibreProviderInfo gRPC method
func (k Keeper) FibreProviderInfo(goCtx context.Context, req *types.QueryFibreProviderInfoRequest) (*types.QueryFibreProviderInfoResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.ValidatorAddress == "" {
		return nil, status.Error(codes.InvalidArgument, "validator address cannot be empty")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	validatorAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddress)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid validator address: %v", err)
	}

	info, found := k.GetFibreProviderInfo(ctx, validatorAddr)
	
	return &types.QueryFibreProviderInfoResponse{
		Info:  &info,
		Found: found,
	}, nil
}

// AllActiveFibreProviders implements the AllActiveFibreProviders gRPC method
func (k Keeper) AllActiveFibreProviders(goCtx context.Context, req *types.QueryAllActiveFibreProvidersRequest) (*types.QueryAllActiveFibreProvidersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	
	providers, err := k.GetAllActiveFibreProviders(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get active fibre providers: %v", err)
	}

	// Handle pagination if requested
	var pagedProviders []types.ActiveFibreProvider
	var pageRes *query.PageResponse
	
	if req.Pagination != nil {
		// Convert providers to a format suitable for pagination
		store := k.getStore(ctx)
		prefixStore := prefix.NewStore(store, types.KeyPrefix(types.FibreProviderInfoKey))
		
		pageRes, err = query.FilteredPaginate(prefixStore, req.Pagination, func(key []byte, value []byte, accumulate bool) (bool, error) {
			if accumulate {
				var info types.FibreProviderInfo
				k.cdc.MustUnmarshal(value, &info)
				
				// Extract validator address from key
				validatorAddr := string(key)
				
				// Check if validator is active
				valAddr, err := sdk.ValAddressFromBech32(validatorAddr)
				if err != nil {
					return false, nil // Skip invalid addresses
				}
				
				isActive, err := k.IsValidatorActive(ctx, valAddr)
				if err != nil || !isActive {
					return false, nil // Skip inactive validators
				}
				
				pagedProviders = append(pagedProviders, types.ActiveFibreProvider{
					ValidatorAddress: validatorAddr,
					Info:             &info,
				})
			}
			return true, nil
		})
		
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	} else {
		pagedProviders = providers
	}

	return &types.QueryAllActiveFibreProvidersResponse{
		Providers:  pagedProviders,
		Pagination: pageRes,
	}, nil
}