package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
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

	// Handle pagination
	page := req.Pagination
	if page == nil {
		page = &query.PageRequest{}
	}

	offset := page.Offset
	limit := page.Limit
	if limit == 0 {
		limit = query.DefaultLimit
	}

	total := uint64(len(providers))
	if offset >= total {
		// out of bounds, return empty list
		providers = []types.ActiveFibreProvider{}
	} else {
		end := offset + limit
		if end > total {
			end = total
		}
		providers = providers[offset:end]
	}

	return &types.QueryAllActiveFibreProvidersResponse{
		Providers: providers,
		Pagination: &query.PageResponse{
			Total: total,
		},
	}, nil
}