package qgb

import (
	"github.com/celestiaorg/celestia-app/x/qgb/keeper"
	"github.com/cosmos/cosmos-sdk/x/staking"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestValsetCreationUponUnbonding(t *testing.T) {
	input, ctx := keeper.SetupFiveValChain(t)
	pk := input.QgbKeeper

	currentValsetNonce := pk.GetLatestValsetNonce(ctx)
	pk.SetValsetRequest(ctx)

	input.Context = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	// begin unbonding
	sh := staking.NewHandler(input.StakingKeeper)
	undelegateMsg := keeper.NewTestMsgUnDelegateValidator(keeper.ValAddrs[0], keeper.StakingAmount)
	sh(input.Context, undelegateMsg)

	// Run the staking endblocker to ensure valset is set in state
	staking.EndBlocker(input.Context, input.StakingKeeper)
	EndBlocker(input.Context, *pk)

	assert.NotEqual(t, currentValsetNonce, pk.GetLatestValsetNonce(ctx))
}
