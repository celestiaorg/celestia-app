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
	maybeUpdateMinter(ctx, k)
	mintBlockProvision(ctx, k)
	setPreviousBlockTime(ctx, k)
}

// maybeSetGenesisTime sets the genesis time if the current block height is 1.
func maybeSetGenesisTime(ctx sdk.Context, k keeper.Keeper) {
	if ctx.BlockHeight() == 1 {
		genesisTime := ctx.BlockTime()
		minter := k.GetMinter(ctx)
		minter.GenesisTime = &genesisTime
		k.SetMinter(ctx, minter)
	}
}

// maybeUpdateMinter updates the inflation rate and annual provisions if the
// inflation rate has changed.
func maybeUpdateMinter(ctx sdk.Context, k keeper.Keeper) {
	minter := k.GetMinter(ctx)
	newInflationRate := minter.CalculateInflationRate(ctx)

	isNonZeroAnnualProvisions := !minter.AnnualProvisions.IsZero()
	if newInflationRate.Equal(minter.InflationRate) && isNonZeroAnnualProvisions {
		// The minter's InflationRate and AnnualProvisions already reflect the
		// values for this year. Exit early because we don't need to update
		// them.
		return
	}

	totalSupply := k.StakingTokenSupply(ctx)
	minter.InflationRate = newInflationRate
	minter.AnnualProvisions = newInflationRate.MulInt(totalSupply)
	k.SetMinter(ctx, minter)
}

// mintBlockProvision mints the block provision for the current block.
func mintBlockProvision(ctx sdk.Context, k keeper.Keeper) {
	minter := k.GetMinter(ctx)
	if minter.PreviousBlockTime == nil {
		// exit early if previous block time is nil
		// this is expected to happen for block height = 1
		return
	}

	toMintCoin := minter.CalculateBlockProvision(ctx.BlockTime(), *minter.PreviousBlockTime)
	toMintCoins := sdk.NewCoins(toMintCoin)

	err := k.MintCoins(ctx, toMintCoins)
	if err != nil {
		panic(err)
	}

	err = k.SendCoinsToFeeCollector(ctx, toMintCoins)
	if err != nil {
		panic(err)
	}

	if toMintCoin.Amount.IsInt64() {
		defer telemetry.ModuleSetGauge(types.ModuleName, float32(toMintCoin.Amount.Int64()), "minted_tokens")
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeMint,
			sdk.NewAttribute(types.AttributeKeyInflationRate, minter.InflationRate.String()),
			sdk.NewAttribute(types.AttributeKeyAnnualProvisions, minter.AnnualProvisions.String()),
			sdk.NewAttribute(sdk.AttributeKeyAmount, toMintCoin.Amount.String()),
		),
	)
}

func setPreviousBlockTime(ctx sdk.Context, k keeper.Keeper) {
	minter := k.GetMinter(ctx)
	blockTime := ctx.BlockTime()
	minter.PreviousBlockTime = &blockTime
	k.SetMinter(ctx, minter)
}
