package app

import (
	"context"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	valaddrkeeper "github.com/celestiaorg/celestia-app/v11/x/valaddr/keeper"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/distribution"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
)

// distributionModule withholds future rewards from active validators that have
// not registered a Fibre host. The SDK sends their unallocated share to the
// community pool without changing other validators' allocations.
type distributionModule struct {
	distribution.AppModule
	keeper        distrkeeper.Keeper
	stakingKeeper *stakingkeeper.Keeper
	valaddrKeeper valaddrkeeper.Keeper
}

func (am distributionModule) BeginBlock(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	version := sdkCtx.ConsensusParams().Version
	if version == nil || version.App < appconsts.FibreRewardsActivationVersion || sdkCtx.BlockHeight() <= 1 {
		return am.AppModule.BeginBlock(ctx)
	}

	votes := sdkCtx.VoteInfos()
	eligible := make([]abci.VoteInfo, 0, len(votes))
	var totalPower int64
	for _, vote := range votes {
		// Keep the original denominator so withholding never increases other
		// validators' rewards. Do not modify the consensus vote information.
		totalPower += vote.Validator.Power
		consAddr := sdk.ConsAddress(vote.Validator.Address)
		validator, err := am.stakingKeeper.GetValidatorByConsAddr(ctx, consAddr)
		if err != nil {
			return err
		}
		if validator.IsBonded() && !validator.IsJailed() {
			registered, err := am.valaddrKeeper.HasFibreProviderInfo(ctx, consAddr)
			if err != nil {
				return err
			}
			if !registered {
				continue
			}
		}
		eligible = append(eligible, vote)
	}

	if err := am.keeper.AllocateTokens(ctx, totalPower, eligible); err != nil {
		return err
	}
	return am.keeper.SetPreviousProposerConsAddr(ctx, sdk.ConsAddress(sdkCtx.BlockHeader().ProposerAddress))
}
