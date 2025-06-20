package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/v4/x/mint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis initializes the x/mint store with data from the genesis state.
func (k Keeper) InitGenesis(ctx context.Context, ak types.AccountKeeper, data *types.GenesisState) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	minter := types.DefaultMinter()
	minter.BondDenom = data.BondDenom
	k.SetMinter(sdkCtx, minter)
	// override the genesis time with the actual genesis time supplied in `InitChain`
	blockTime := sdkCtx.BlockTime()
	gt := types.GenesisTime{
		GenesisTime: &blockTime,
	}
	k.SetGenesisTime(sdkCtx, gt)
	// Although ak.GetModuleAccount appears to be a no-op, it actually creates a
	// new module account in the x/auth account store if it doesn't exist. See
	// the x/auth keeper for more details.
	ak.GetModuleAccount(sdkCtx, types.ModuleName)

	return nil
}

// ExportGenesis returns a x/mint GenesisState for the given context.
func (k Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	bondDenom := k.GetMinter(sdkCtx).BondDenom
	return types.NewGenesisState(bondDenom)
}
