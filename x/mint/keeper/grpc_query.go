package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/v4/x/mint/types"
)

var _ types.QueryServer = Keeper{}

// InflationRate returns minter.InflationRate of the mint module.
func (k Keeper) InflationRate(c context.Context, _ *types.QueryInflationRateRequest) (*types.QueryInflationRateResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	minter := k.GetMinter(ctx)

	return &types.QueryInflationRateResponse{InflationRate: minter.InflationRate}, nil
}

// AnnualProvisions returns minter.AnnualProvisions of the mint module.
func (k Keeper) AnnualProvisions(c context.Context, _ *types.QueryAnnualProvisionsRequest) (*types.QueryAnnualProvisionsResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	minter := k.GetMinter(ctx)

	return &types.QueryAnnualProvisionsResponse{AnnualProvisions: minter.AnnualProvisions}, nil
}

// GenesisTime returns the GensisTime of the mint module.
func (k Keeper) GenesisTime(c context.Context, _ *types.QueryGenesisTimeRequest) (*types.QueryGenesisTimeResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	genesisTime := k.GetGenesisTime(ctx).GenesisTime

	return &types.QueryGenesisTimeResponse{GenesisTime: genesisTime}, nil
}
