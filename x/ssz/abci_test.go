package ssz_test

import (
	"fmt"
	"testing"

	// "time"

	"github.com/celestiaorg/celestia-app/x/ssz"

	// "github.com/cosmos/cosmos-sdk/x/staking/teststaking"
	// stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	testutil "github.com/celestiaorg/celestia-app/test/util"

	// "github.com/cosmos/cosmos-sdk/x/staking"
	// stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"

	"github.com/stretchr/testify/require"
)

func TestMerkleProofs(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	sszKeeper := input.SSZKeeper

	ctx = ctx.WithBlockHeight(1)
	// expectedTime := ctx.BlockTime()
	// EndBlocker should set a new validator set
	ssz.EndBlocker(ctx, *sszKeeper)

	latestHash := sszKeeper.GetSSZHash(ctx)
	fmt.Printf("LatestSSZHash: %v\n", latestHash)
	require.NotNil(t, latestHash)

	// TODO query here for the proof of SSZ hash against app hash
	// TODO verify the merkle proof

	// TODO query here for the proof of the app hash against the latest header
	// TODO verify the merkle proof against latest header

}
