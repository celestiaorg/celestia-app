package qgb

import (
	"errors"
	"fmt"

	sdkerrors "cosmossdk.io/errors"

	"github.com/celestiaorg/celestia-app/x/qgb/keeper"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// SignificantPowerDifferenceThreshold the threshold of change in the validator set power
	// that would need the creation of a new valset request.
	SignificantPowerDifferenceThreshold = 0.05

	// AttestationPruningThreshold the minimum number of recent attestations that will always be present
	// in state.
	AttestationPruningThreshold = 100
)

// EndBlocker is called at the end of every block.
func EndBlocker(ctx sdk.Context, k keeper.Keeper) {
	// we always want to create the valset at first so that if there is a new validator set, then it is
	// the one responsible for signing from now on.
	handleValsetRequest(ctx, k)
	handleDataCommitmentRequest(ctx, k)
	PruneIfNeeded(ctx, k)
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
	// this will  keep executing until all the needed data commitments are created and we catchup to the current height
	for {
		hasLastDataCommitment, err := k.HasDataCommitmentInStore(ctx)
		if err != nil {
			panic(err)
		}
		if hasLastDataCommitment {
			// if the store already has a data commitment, we use it to check if we need to create a new data commitment
			lastDataCommitment, err := k.GetLastDataCommitment(ctx)
			if err != nil {
				panic(err)
			}
			if ctx.BlockHeight()-int64(lastDataCommitment.EndBlock) >= int64(k.GetDataCommitmentWindowParam(ctx)) {
				setDataCommitmentAttestation()
			} else {
				// the needed data commitments are already created and we need to wait for the next window to elapse
				break
			}
		} else {
			// if the store doesn't have a data commitment, we check if the window has passed to create a new data commitment
			if ctx.BlockHeight() >= int64(k.GetDataCommitmentWindowParam(ctx)) {
				setDataCommitmentAttestation()
			} else {
				// the first data commitment window hasn't elapsed yet to create a commitment
				break
			}
		}
	}
}

func handleValsetRequest(ctx sdk.Context, k keeper.Keeper) {
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

		significantPowerDiff = intCurrMembers.PowerDiff(*intLatestMembers) > SignificantPowerDifferenceThreshold
	}

	if (latestValset == nil) || (lastUnbondingHeight == uint64(ctx.BlockHeight())) || significantPowerDiff {
		// if the conditions are true, put in a new validator set request to be signed and submitted to EVM
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

func PruneIfNeeded(ctx sdk.Context, k keeper.Keeper) {
	// If the attestations nonce hasn't been initialized yet, no pruning is
	// required
	if !k.CheckLatestAttestationNonce(ctx) {
		return
	}
	// If the nonce is not greater than the minimum number of attestations,
	// no pruning is required.
	if k.GetLatestAttestationNonce(ctx) <= AttestationPruningThreshold {
		return
	}

	lastAvailableNonce := k.GetLastPrunedAttestationNonce(ctx) + 1
	lastAttestationInStore, found, err := k.GetAttestationByNonce(ctx, lastAvailableNonce)
	if err != nil {
		panic(err)
	}
	if !found {
		panic(fmt.Sprintf("couldn't find attestation %s for pruning", lastAttestationInStore))
	}
	var lastNonceHeight uint64
	switch lastAttestationInStore.Type() {
	case types.DataCommitmentRequestType:
		dc := lastAttestationInStore.(*types.DataCommitment)
		lastNonceHeight = dc.EndBlock
	case types.ValsetRequestType:
		vs := lastAttestationInStore.(*types.Valset)
		lastNonceHeight = vs.Height
	}
	lastUnbondingHeight := k.GetLastUnBondingBlockHeight(ctx)
	if lastNonceHeight == lastUnbondingHeight {
		// we don't need to do anything, we want to keep attestations up to the last unbonding height
		return
	}
	if lastNonceHeight > lastUnbondingHeight {
		// checking if it's the initial case following the startup of the chain
		if lastNonceHeight == 1 && lastUnbondingHeight == 0 {
			return
		}
		// we should never hit this case, since we will keep the attestations up to the last unbonding height
		panic("missing attestations up to the unbonding height")
	}
	// now we have attestations before the unbonding height, we should check whether we need to prune them
	// or not yet
	latestAttestationNonce := k.GetLatestAttestationNonce(ctx)
	if latestAttestationNonce-lastAvailableNonce+1 <= AttestationPruningThreshold {
		// we don't need to prune as we still have room to store more attestations
		return
	}
	// now we want to prune attestations as we have the following conditions:
	// - we have attestations up to the last unbonding height
	// - the total number of attestations, including the ones before the unbonding height, are greater
	// than the AttestationPruningThreshold
	err = pruneAttestations(ctx, k, lastAvailableNonce, latestAttestationNonce)
	if err != nil {
		panic(err)
	}
}

// pruneAttestations prunes attestations to keep attestations up to the last unbonding height
// and the total number of attestations lower than the AttestationPruningThreshold.
func pruneAttestations(
	ctx sdk.Context,
	k keeper.Keeper,
	lastAvailableAttestationNonce uint64,
	latestAttestationNonce uint64,
) error {
	ctx.Logger().Debug("pruning attestations from qgb store")
	if !k.CheckLastUnbondingNonce(ctx) {
		// We should never hit this case in the happy path because at this level, we're sure that
		// all store values are initialized.
		// However, we might hit it in an upgrade if we don't handle the store values carefully.
		// So, it's good to have this check in place.
		return fmt.Errorf("last unbonding nonce not initialized in store")
	}
	lastUnbondingNonce := k.GetLastUnbondingNonce(ctx)
	count := 0
	newLastAvailableNonce := lastAvailableAttestationNonce
	for i := lastAvailableAttestationNonce; i < lastUnbondingNonce; i++ {
		if newLastAvailableNonce == lastUnbondingNonce ||
			latestAttestationNonce-i+1 == AttestationPruningThreshold {
			// we either reached the last unbonding height, and we need to keep all those attestations
			// or, we reached the minimum number of attestations we want to keep in store
			newLastAvailableNonce = i
			break
		}
		k.DeleteAttestation(ctx, i)
		count++
	}
	// persist the new last pruned attestation nonce
	k.SetLastPrunedAttestationNonce(ctx, newLastAvailableNonce-1)
	ctx.Logger().Debug(
		"finished pruning attestations from qgb store",
		"count",
		count,
		"new_last_available_nonce",
		newLastAvailableNonce,
		"latest_attestation_nonce",
		latestAttestationNonce,
	)
	return nil
}
