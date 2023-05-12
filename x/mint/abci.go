package mint

import (
	"time"

	"github.com/celestiaorg/celestia-app/x/mint/keeper"
	"github.com/celestiaorg/celestia-app/x/mint/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BeginBlocker updates the inflation rate, annual provisions, and then mints
// the block provision for the current block.
func BeginBlocker(ctx sdk.Context, k keeper.Keeper) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyBeginBlocker)

	maybeSetGenesisTime(ctx, k)
	updateInflationRate(ctx, k)
	updateAnnualProvisions(ctx, k)
	mintBlockProvision(ctx, k)
}

func maybeSetGenesisTime(ctx sdk.Context, k keeper.Keeper) {
	if ctx.BlockHeight() == 1 {
		genesisTime := ctx.BlockTime()
		minter := k.GetMinter(ctx)
		minter.GenesisTime = &genesisTime
		k.SetMinter(ctx, minter)
	}
}

func updateInflationRate(ctx sdk.Context, k keeper.Keeper) {
	// TODO: since the inflation rate only changes once per year, we don't need
	// to perform this every block. One potential optimization is to only do
	// this once per year.
	minter := k.GetMinter(ctx)
	minter.InflationRate = minter.CalculateInflationRate(ctx)
	k.SetMinter(ctx, minter)
}

func updateAnnualProvisions(ctx sdk.Context, k keeper.Keeper) {
	// TODO: only perform this once per year.
	minter := k.GetMinter(ctx)
	totalSupply := k.StakingTokenSupply(ctx)
	minter.AnnualProvisions = minter.CalculateAnnualProvisions(totalSupply)
	k.SetMinter(ctx, minter)
}

func mintBlockProvision(ctx sdk.Context, k keeper.Keeper) {
	minter := k.GetMinter(ctx)
	mintedCoin := minter.CalculateBlockProvision()
	mintedCoins := sdk.NewCoins(mintedCoin)

	err := k.MintCoins(ctx, mintedCoins)
	if err != nil {
		panic(err)
	}

	err = k.SendCoinsToFeeCollector(ctx, mintedCoins)
	if err != nil {
		panic(err)
	}

	if mintedCoin.Amount.IsInt64() {
		defer telemetry.ModuleSetGauge(types.ModuleName, float32(mintedCoin.Amount.Int64()), "minted_tokens")
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeMint,
			sdk.NewAttribute(types.AttributeKeyInflationRate, minter.InflationRate.String()),
			sdk.NewAttribute(types.AttributeKeyAnnualProvisions, minter.AnnualProvisions.String()),
			sdk.NewAttribute(sdk.AttributeKeyAmount, mintedCoin.Amount.String()),
		),
	)
}
