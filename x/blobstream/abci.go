package blobstream

import (
	"errors"
	"time"

	sdkerrors "cosmossdk.io/errors"

	"github.com/celestiaorg/celestia-app/x/blobstream/keeper"
	"github.com/celestiaorg/celestia-app/x/blobstream/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	oneDay  = 24 * time.Hour
	oneWeek = 7 * oneDay
	// AttestationExpiryTime is the expiration time of an attestation. When this
	// much time has passed after an attestation has been published, it will be
	// pruned from state.
	AttestationExpiryTime = 3 * oneWeek // 3 weeks
)

// SignificantPowerDifferenceThreshold is the threshold of change in the
// validator set power that would trigger the creation of a new valset
// request.
var SignificantPowerDifferenceThreshold = sdk.NewDecWithPrec(5, 2) // 0.05

// EndBlocker is called at the end of every block.
func EndBlocker(ctx sdk.Context, k keeper.Keeper) {
	// we always want to create the valset at first so that if there is a new
	// validator set, then it is the one responsible for signing from now on.
	handleValsetRequest(ctx, k)
	handleDataCommitmentRequest(ctx, k)
	pruneAttestations(ctx, k)
}

func handleDataCommitmentRequest(ctx sdk.Context, k keeper.Keeper) {
	setDataCommitmentAttestation := func() {
		dataCommitment, err := k.NextDataCommitment(ctx)
		if err != nil {
			panic(sdkerrors.Wrap(err, "couldn't get current data commitment"))
		}
		err = k.SetAttestationRequest(ctx, &dataCommitment)
		if err != nil {
			panic(err)
		}
	}
	dataCommitmentWindow := int64(k.GetDataCommitmentWindowParam(ctx))
	// this will keep executing until all the needed data commitments are
	// created and we catchup to the current height
	for {
		hasLatestDataCommitment, err := k.HasDataCommitmentInStore(ctx)
		if err != nil {
			panic(err)
		}
		if hasLatestDataCommitment {
			// if the store already has a data commitment, we use it to check if
			// we need to create a new data commitment
			latestDataCommitment, err := k.GetLatestDataCommitment(ctx)
			if err != nil {
				panic(err)
			}
			if ctx.BlockHeight()-int64(latestDataCommitment.EndBlock) >= dataCommitmentWindow {
				setDataCommitmentAttestation()
			} else {
				// the needed data commitments are already created and we need
				// to wait for the next window to elapse
				break
			}
		} else {
			// if the store doesn't have a data commitment, we check if the
			// window has passed to create a new data commitment
			if ctx.BlockHeight() >= dataCommitmentWindow {
				setDataCommitmentAttestation()
			} else {
				// the first data commitment window hasn't elapsed yet to create
				// a commitment
				break
			}
		}
	}
}

func handleValsetRequest(ctx sdk.Context, k keeper.Keeper) {
	// get the latest valsets to compare against
	var latestValset *types.Valset
	if k.CheckLatestAttestationNonce(ctx) && k.GetLatestAttestationNonce(ctx) != 0 {
		var err error
		latestValset, err = k.GetLatestValset(ctx)
		if err != nil {
			panic(err)
		}
	}

	latestUnbondingHeight := k.GetLatestUnBondingBlockHeight(ctx)

	significantPowerDiff := false
	if latestValset != nil {
		vs, err := k.GetCurrentValset(ctx)
		if err != nil {
			// this condition should only occur in the simulator ref :
			// https://github.com/Gravity-Bridge/Gravity-Bridge/issues/35
			if errors.Is(err, types.ErrNoValidators) {
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

		significantPowerDiff = intCurrMembers.PowerDiff(*intLatestMembers).GT(SignificantPowerDifferenceThreshold)

	}

	if (latestValset == nil) || (latestUnbondingHeight == uint64(ctx.BlockHeight())) || significantPowerDiff {
		// if the conditions are true, put in a new validator set request to be
		// signed and submitted to EVM
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

// pruneAttestations runs basic checks on saved attestations to see if we need
// to prune or not. Then, it prunes all expired attestations from state.
func pruneAttestations(ctx sdk.Context, k keeper.Keeper) {
	// If the attestation nonce hasn't been initialized yet, no pruning is
	// required
	if !k.CheckLatestAttestationNonce(ctx) {
		return
	}
	if !k.CheckEarliestAvailableAttestationNonce(ctx) {
		ctx.Logger().Error("couldn't find earliest attestation for pruning")
		return
	}

	currentBlockTime := ctx.BlockTime()
	latestAttestationNonce := k.GetLatestAttestationNonce(ctx)
	earliestNonce := k.GetEarliestAvailableAttestationNonce(ctx)
	var newEarliestAvailableNonce uint64
	for newEarliestAvailableNonce = earliestNonce; newEarliestAvailableNonce < latestAttestationNonce; newEarliestAvailableNonce++ {
		newEarliestAttestation, found, err := k.GetAttestationByNonce(ctx, newEarliestAvailableNonce)
		if err != nil {
			ctx.Logger().Error("error getting attestation for pruning", "nonce", newEarliestAvailableNonce, "err", err.Error())
			return
		}
		if !found {
			ctx.Logger().Error("couldn't find attestation for pruning", "nonce", newEarliestAvailableNonce)
			return
		}
		if newEarliestAttestation == nil {
			ctx.Logger().Error("nil attestation for pruning", "nonce", newEarliestAvailableNonce)
			return
		}
		attestationExpirationTime := newEarliestAttestation.BlockTime().Add(AttestationExpiryTime)
		if attestationExpirationTime.After(currentBlockTime) {
			// the current attestation is unexpired so subsequent ones are also
			// unexpired persist the new earliest available attestation nonce
			break
		}
		k.DeleteAttestation(ctx, newEarliestAvailableNonce)
	}
	if newEarliestAvailableNonce > earliestNonce {
		// some attestations were pruned and we need to update the state for it
		k.SetEarliestAvailableAttestationNonce(ctx, newEarliestAvailableNonce)
		ctx.Logger().Debug(
			"pruned attestations from Blobstream store",
			"count",
			newEarliestAvailableNonce-earliestNonce,
			"new_earliest_available_nonce",
			newEarliestAvailableNonce,
			"latest_attestation_nonce",
			latestAttestationNonce,
		)
	}
}
