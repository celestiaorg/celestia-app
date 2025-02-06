package mint

import (
	"time"

	"github.com/celestiaorg/celestia-app/v4/x/mint/keeper"
	"github.com/celestiaorg/celestia-app/v4/x/mint/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BeginBlocker updates the inflation rate, annual provisions, and then mints
// the block provision for the current block.
func BeginBlocker(ctx sdk.Context, k keeper.Keeper) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyBeginBlocker)

	maybeUpdateMinter(ctx, k)
	mintBlockProvision(ctx, k)
	setPreviousBlockTime(ctx, k)
}

// maybeUpdateMinter updates the inflation rate and annual provisions if the
// inflation rate has changed. The inflation rate is expected to change once per
// year at the genesis time anniversary until the TargetInflationRate is
// reached.
func maybeUpdateMinter(ctx sdk.Context, k keeper.Keeper) {
	minter := k.GetMinter(ctx)
	genesisTime := k.GetGenesisTime(ctx).GenesisTime
	newInflationRate := minter.CalculateInflationRate(ctx, *genesisTime)

	isNonZeroAnnualProvisions := !minter.AnnualProvisions.IsZero()
	if newInflationRate.Equal(minter.InflationRate) && isNonZeroAnnualProvisions {
		// The minter's InflationRate and AnnualProvisions already reflect the
		// values for this year. Exit early because we don't need to update
		// them. AnnualProvisions must be updated if it is zero (expected at
		// genesis).
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

	toMintCoin, err := minter.CalculateBlockProvision(ctx.BlockTime(), *minter.PreviousBlockTime)
	if err != nil {
		panic(err)
	}
	toMintCoins := sdk.NewCoins(toMintCoin)

	err = k.MintCoins(ctx, toMintCoins)
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
