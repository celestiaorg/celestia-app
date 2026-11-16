package keeper

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v11/x/mint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis initializes the x/mint store with data from the genesis state.
func (k Keeper) InitGenesis(ctx context.Context, ak types.AccountKeeper, data *types.GenesisState) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if err := types.ValidateGenesis(*data); err != nil {
		return fmt.Errorf("invalid %s genesis: %w", types.ModuleName, err)
	}

	minter := types.DefaultMinter()
	minter.BondDenom = data.BondDenom
	// Without an exported minter, the genesis time is the one supplied in `InitChain`.
	genesisTime := sdkCtx.BlockTime()
	if data.Minter != nil {
		if data.GenesisTime.After(genesisTime) {
			return fmt.Errorf("invalid %s genesis: genesis time %s is after block time %s", types.ModuleName, *data.GenesisTime, genesisTime)
		}
		minter = *data.Minter
		// The first block after import mints nothing.
		minter.PreviousBlockTime = nil
		genesisTime = *data.GenesisTime
	}
	k.SetMinter(sdkCtx, minter)
	k.SetGenesisTime(sdkCtx, types.GenesisTime{GenesisTime: &genesisTime})
	// Although ak.GetModuleAccount appears to be a no-op, it actually creates a
	// new module account in the x/auth account store if it doesn't exist. See
	// the x/auth keeper for more details.
	ak.GetModuleAccount(sdkCtx, types.ModuleName)

	return nil
}

// ExportGenesis returns a x/mint GenesisState for the given context.
func (k Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	minter := k.GetMinter(sdkCtx)
	minter.PreviousBlockTime = nil
	gs := types.NewGenesisState(minter.BondDenom)
	gs.Minter = &minter
	gs.GenesisTime = k.GetGenesisTime(sdkCtx).GenesisTime
	return gs
}
