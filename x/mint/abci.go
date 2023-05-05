package mint

import (
	"time"

	"github.com/celestiaorg/celestia-app/x/mint/keeper"
	"github.com/celestiaorg/celestia-app/x/mint/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BeginBlocker mints new tokens for the previous block.
func BeginBlocker(ctx sdk.Context, k keeper.Keeper) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyBeginBlocker)

	minter := k.GetMinter(ctx)

	// Recalculate the inflation rate and annual provisions and update the minter with the new values.
	totalSupply := k.StakingTokenSupply(ctx)
	// TODO: since the inflation rate only changes once per year, we don't need
	// to perform this every block. One potential optimization is to only do
	// this once per year.
	minter.InflationRate = minter.CalculateInflationRate(ctx)
	minter.AnnualProvisions = minter.CalculateAnnualProvisions(totalSupply)
	if ctx.BlockHeight() == 0 {
		genesisTime := ctx.BlockTime()
		minter.GenesisTime = &genesisTime
	}
	k.SetMinter(ctx, minter)

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
