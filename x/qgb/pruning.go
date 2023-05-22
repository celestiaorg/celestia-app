package qgb

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/x/qgb/keeper"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	v1 "github.com/celestiaorg/celestia-app/x/qgb/v1"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// PruneIfNeeded runs basic checks on saved attestations to see if pruning is
// required. Pruning occurs for attestations that were set from a height that
// surpasses the PruningThreshold (220_000 blocks). This function will always
// stop the state machine from pruning the oldest validator set.
func PruneIfNeeded(ctx sdk.Context, k keeper.Keeper) {
	// If the attestations nonce hasn't been initialized yet, no pruning is
	// required
	if !k.CheckLatestAttestationNonce(ctx) {
		return
	}

	// always keep the oldest validator set update, which gets set at least
	// every time a validator reaches the unbonding height. This covers the edge
	// case where there are no validator set updates for the pruning period. In
	// this case, we don't want to prune because we want the relayer to have a
	// snapshot of the evm version of the validator set at that height.
	oldestNonce := k.GetOldestAttestationNonce(ctx)
	oldestNonceHeight := getOldestNonceHeight(ctx, k, oldestNonce)
	newestValsetUpdate := k.GetValsetUpdateHeight(ctx)
	if oldestNonceHeight == int64(newestValsetUpdate) {
		return
	}

	// pruning is only needed if there are attestations that exceed the threshold
	if int64(oldestNonceHeight) > ctx.BlockHeader().Height-PruningThreshold(ctx.BlockHeader().Version.App) {
		return
	}

	// prune a single attestation per block. combined with the above check, this
	// ensures that the last valset update is never deleted until there is at
	// least one valset
	pruneAttestation(ctx, k)
}

// getOldestNonceHeight returns the block height corresponding to the last available
// nonce in store.
func getOldestNonceHeight(ctx sdk.Context, k keeper.Keeper, nonce uint64) int64 {
	lastAttestationInStore, found, err := k.GetAttestationByNonce(ctx, nonce)
	if err != nil {
		panic(err)
	}
	if !found {
		panic(fmt.Sprintf("couldn't find attestation %s for pruning", lastAttestationInStore))
	}
	switch lastAttestationInStore.Type() {
	case types.DataCommitmentRequestType:
		dc := lastAttestationInStore.(*types.DataCommitment)
		return int64(dc.EndBlock)
	case types.ValsetRequestType:
		vs := lastAttestationInStore.(*types.Valset)
		return int64(vs.Height)
	default:
		panic("unknown attestation type")
	}
}

// pruneAttestation prunes attestations to keep attestations up to the last unbonding height
// and the total number of attestations lower than the AttestationPruningThreshold.
func pruneAttestation(ctx sdk.Context, k keeper.Keeper) {
	oldestNonce := k.GetOldestAttestationNonce(ctx)
	k.DeleteAttestation(ctx, oldestNonce)
	ctx.Logger().Debug(
		"pruned attestation from qgb store",
		"nonce",
		oldestNonce,
	)

	// persist the new last available attestation nonce
	oldestNonce++
	k.SetOldestAttestationNonce(ctx, uint64(oldestNonce))

}

var (
	testPruningThreshold int64 = 0
)

// SetTestPruningThreshold sets the pruning threshold. NOTE: for testing
// puruposes only. Calling this
func SetTestPruningThreshold(threshold int64) {
	testPruningThreshold = threshold
}

// PruningThreshold returns the PruningThreshold constant depending on app
// version and context.
//
// NOTE: if the appversion is set to 0, and SetTestPruningThreshold, then that
// value will be returned.
func PruningThreshold(version uint64) int64 {
	if testPruningThreshold != 0 && version == 0 {
		return int64(testPruningThreshold)
	}
	return v1.PruningThreshold
}
