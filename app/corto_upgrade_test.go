package app_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/frozen"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/stretchr/testify/require"
)

// TestCortoUnbondingTime checks that the native upgrade shortens unbonding on
// Corto only.
func TestCortoUnbondingTime(t *testing.T) {
	for _, chainID := range []string{appconsts.CortoChainID, appconsts.MochaChainID, appconsts.MainnetChainID} {
		t.Run(chainID, func(t *testing.T) {
			testApp, _, _ := util.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
			ctx := testApp.NewContext(false).WithBlockHeight(1).WithChainID(chainID)
			before, err := testApp.StakingKeeper.GetParams(ctx)
			require.NoError(t, err)
			want := before.UnbondingTime
			if chainID == appconsts.CortoChainID {
				want = appconsts.CortoUnbondingTime
			}

			require.NoError(t, testApp.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{
				Name:   fmt.Sprintf("v%d", appconsts.Version),
				Height: ctx.BlockHeight(),
			}))

			params, err := testApp.StakingKeeper.GetParams(ctx)
			require.NoError(t, err)
			require.Equal(t, want, params.UnbondingTime)

			// A new undelegation completes after the chain's unbonding time.
			vals, err := testApp.StakingKeeper.GetAllValidators(ctx)
			require.NoError(t, err)
			valAddr, err := sdk.ValAddressFromBech32(vals[0].OperatorAddress)
			require.NoError(t, err)
			dels, err := testApp.StakingKeeper.GetValidatorDelegations(ctx, valAddr)
			require.NoError(t, err)
			delAddr := sdk.MustAccAddressFromBech32(dels[0].DelegatorAddress)
			completion, amount, err := testApp.StakingKeeper.Undelegate(ctx, delAddr, valAddr, dels[0].Shares.QuoInt64(2))
			require.NoError(t, err)
			require.Equal(t, ctx.BlockTime().Add(want), completion)

			// The staking end blocker pays the tokens out at maturity.
			balance := testApp.BankKeeper.GetBalance(ctx, delAddr, appconsts.BondDenom).Amount
			matured := ctx.WithBlockTime(completion).WithBlockHeight(ctx.BlockHeight() + 1)
			_, err = testApp.StakingKeeper.EndBlocker(matured)
			require.NoError(t, err)
			require.Equal(t, balance.Add(amount), testApp.BankKeeper.GetBalance(matured, delAddr, appconsts.BondDenom).Amount)

			if chainID != appconsts.CortoChainID {
				return
			}
			// If the delegator is listed, the payout can only go to the whitehat.
			testApp.FrozenRules.SetNetwork(chainID, frozen.CortoWhitehat, delAddr)
			v11 := matured.WithConsensusParams(cmtproto.ConsensusParams{Version: &cmtproto.VersionParams{App: appconsts.Version}})
			payout := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, amount))
			require.ErrorIs(t, testApp.BankKeeper.SendCoins(v11, delAddr, sdk.AccAddress(bytes.Repeat([]byte{9}, 20)), payout), frozen.ErrFrozen)
			require.NoError(t, testApp.BankKeeper.SendCoins(v11, delAddr, sdk.MustAccAddressFromBech32(frozen.CortoWhitehat), payout))
		})
	}
}

// A real v11 upgrade records its completion. A later native upgrade must not
// overwrite a governance change made after that completed migration.
func TestCortoUnbondingPreservedAfterV11(t *testing.T) {
	testApp, _, _ := util.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(false).WithBlockHeight(1).WithChainID(appconsts.CortoChainID)
	headerInfo := ctx.HeaderInfo()
	headerInfo.Height = 1
	ctx = ctx.WithHeaderInfo(headerInfo)
	testApp.UpgradeKeeper.SetUpgradeHandler("v11", func(_ context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return fromVM, nil
	})
	require.NoError(t, testApp.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: "v11", Height: 1}))
	doneHeight, err := testApp.UpgradeKeeper.GetDoneHeight(ctx, "v11")
	require.NoError(t, err)
	require.Equal(t, int64(1), doneHeight)
	testApp.RegisterUpgradeHandlers()
	params, err := testApp.StakingKeeper.GetParams(ctx)
	require.NoError(t, err)
	// This is the value permitted by the staking governance parameter filter.
	params.UnbondingTime = appconsts.UnbondingTime
	require.NoError(t, testApp.StakingKeeper.SetParams(ctx, params))
	ctx = ctx.WithBlockHeight(2)
	require.NoError(t, testApp.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: fmt.Sprintf("v%d", appconsts.Version), Height: 2}))
	got, err := testApp.StakingKeeper.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, params.UnbondingTime, got.UnbondingTime)
}
