package keeper

import (
	"context"
	"time"

	"github.com/celestiaorg/celestia-app/v4/x/mint/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BeginBlocker updates the inflation rate, annual provisions, and then mints
// the block provision for the current block.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyBeginBlocker)

	maybeUpdateMinter(sdkCtx, k)
	if err := mintBlockProvision(sdkCtx, k); err != nil {
		return err
	}

	setPreviousBlockTime(sdkCtx, k)

	return nil
}

// maybeUpdateMinter updates the inflation rate and annual provisions if the
// inflation rate has changed. The inflation rate is expected to change once per
// year at the genesis time anniversary until the TargetInflationRate is
// reached.
func maybeUpdateMinter(ctx sdk.Context, k Keeper) {
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
func mintBlockProvision(ctx sdk.Context, k Keeper) error {
	minter := k.GetMinter(ctx)
	if minter.PreviousBlockTime == nil {
		// exit early if previous block time is nil
		// this is expected to happen for block height = 1
		return nil
	}

	toMintCoin, err := minter.CalculateBlockProvision(ctx.BlockTime(), *minter.PreviousBlockTime)
	if err != nil {
		return err
	}
	toMintCoins := sdk.NewCoins(toMintCoin)

	err = k.MintCoins(ctx, toMintCoins)
	if err != nil {
		return err
	}

	err = k.SendCoinsToFeeCollector(ctx, toMintCoins)
	if err != nil {
		return err
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

	return nil
}

func setPreviousBlockTime(ctx sdk.Context, k Keeper) {
	minter := k.GetMinter(ctx)
	blockTime := ctx.BlockTime()

	minter.PreviousBlockTime = &blockTime
	k.SetMinter(ctx, minter)
}
