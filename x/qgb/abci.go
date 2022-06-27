package qgb

import (
	"github.com/celestiaorg/celestia-app/x/qgb/keeper"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// EndBlocker is called at the end of every block
func EndBlocker(ctx sdk.Context, k keeper.Keeper) {
	handleDataCommitmentRequest(ctx, k)
	handleValsetRequest(ctx, k)
}

func handleDataCommitmentRequest(ctx sdk.Context, k keeper.Keeper) {
	if ctx.BlockHeight() != 0 && ctx.BlockHeight()%int64(types.DataCommitmentWindow) == 0 {
		dataCommitment, err := k.GetCurrentDataCommitment(ctx)
		if err != nil {
			panic(sdkerrors.Wrap(err, "coudln't get current data commitment"))
		}
		err = k.SetAttestationRequest(ctx, &dataCommitment)
		if err != nil {
			panic(err)
		}
	}
}

func handleValsetRequest(ctx sdk.Context, k keeper.Keeper) {
	// FIXME can this be done here?
	// We need to initialize this value in the begining...
	if !k.CheckLatestAttestationNonce(ctx) {
		k.SetLatestAttestationNonce(ctx, 0)
	}
	// get the last valsets to compare against
	var latestValset *types.Valset
	if k.CheckLatestAttestationNonce(ctx) && k.GetLatestAttestationNonce(ctx) != 0 {
		var err error
		latestValset, err = k.GetLatestValset(ctx)
		if err != nil {
			panic(err)
		}
	}

	lastUnbondingHeight := k.GetLastUnBondingBlockHeight(ctx)

	significantPowerDiff := false
	if latestValset != nil {
		vs, err := k.GetCurrentValset(ctx)
		if err != nil {
			// this condition should only occur in the simulator
			// ref : https://github.com/Gravity-Bridge/Gravity-Bridge/issues/35
			if err == types.ErrNoValidators {
				ctx.Logger().Error("no bonded validators",
					"cause", err.Error(),
				)
				return
			}
			panic(err)
		}
		intCurrMembers, err := types.BridgeValidators(vs.Members).ToInternal()
		if err != nil {
			panic(sdkerrors.Wrap(err, "invalid current valset members"))
		}
		intLatestMembers, err := types.BridgeValidators(latestValset.Members).ToInternal()
		if err != nil {
			panic(sdkerrors.Wrap(err, "invalid latest valset members"))
		}

		significantPowerDiff = intCurrMembers.PowerDiff(*intLatestMembers) > 0.05
	}

	if (latestValset == nil) || (lastUnbondingHeight == uint64(ctx.BlockHeight())) || significantPowerDiff {
		// if the conditions are true, put in a new validator set request to be signed and submitted to Ethereum
		valset, err := k.GetCurrentValset(ctx)
		if err != nil {
			panic(err)
		}
		err = k.SetAttestationRequest(ctx, &valset)
		if err != nil {
			panic(err)
		}
	}
}
