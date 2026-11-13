package app_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	"github.com/stretchr/testify/require"
)

func applyV11FromFibreV1(t *testing.T, testApp *app.App, ctx sdk.Context) error {
	t.Helper()
	versions, err := testApp.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.NoError(t, err)
	versions[fibretypes.ModuleName] = 1
	require.NoError(t, testApp.UpgradeKeeper.SetModuleVersionMap(ctx, versions))
	// Like FinalizeBlock, commit the enclosing context only if the upgrade
	// succeeds. The migration must not allocate its own full-state cache.
	upgradeCtx, write := ctx.CacheContext()
	if err := testApp.UpgradeKeeper.ApplyUpgrade(upgradeCtx, upgradetypes.Plan{Name: "v11", Height: ctx.BlockHeight()}); err != nil {
		return err
	}
	write()
	return nil
}

func fundFibreModule(t *testing.T, testApp *app.App, ctx sdk.Context, coins sdk.Coins) {
	t.Helper()
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coins))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, fibretypes.ModuleName, coins))
}

func TestV11UpgradeMigratesFibreEscrows(t *testing.T) {
	testApp, _, _ := util.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(false).WithBlockHeight(1).WithBlockTime(util.GenesisTime.Add(time.Hour))
	owner := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	other := sdk.AccAddress(bytes.Repeat([]byte{2}, 20))
	lower, upper := owner.String(), strings.ToUpper(owner.String())
	k := testApp.FibreKeeper
	for _, account := range []fibretypes.EscrowAccount{
		{Signer: lower, Balance: sdk.NewInt64Coin("utia", 1000), AvailableBalance: sdk.NewInt64Coin("utia", 700)},
		{Signer: upper, Balance: sdk.NewInt64Coin("utia", 500), AvailableBalance: sdk.NewInt64Coin("utia", 200)},
		{Signer: other.String(), Balance: sdk.NewInt64Coin("utia", 400), AvailableBalance: sdk.NewInt64Coin("utia", 400)},
		{Signer: strings.ToUpper(other.String()), Balance: sdk.NewInt64Coin("stake", 700), AvailableBalance: sdk.NewInt64Coin("stake", 500)},
	} {
		k.SetEscrowAccount(ctx, account)
	}
	requested := ctx.BlockTime().Add(-time.Minute)
	withdrawals := []fibretypes.Withdrawal{
		{Signer: lower, Amount: sdk.NewInt64Coin("utia", 300), RequestedTimestamp: requested, AvailableTimestamp: ctx.BlockTime().Add(time.Hour)},
		{Signer: upper, Amount: sdk.NewInt64Coin("utia", 200), RequestedTimestamp: requested, AvailableTimestamp: ctx.BlockTime().Add(2 * time.Hour)},
		{Signer: upper, Amount: sdk.NewInt64Coin("utia", 100), RequestedTimestamp: requested.Add(time.Second), AvailableTimestamp: ctx.BlockTime().Add(3 * time.Hour)},
		{Signer: strings.ToUpper(other.String()), Amount: sdk.NewInt64Coin("stake", 200), RequestedTimestamp: requested, AvailableTimestamp: ctx.BlockTime().Add(time.Hour)},
	}
	for _, withdrawal := range withdrawals {
		k.SetWithdrawal(ctx, withdrawal)
	}
	fundFibreModule(t, testApp, ctx, sdk.NewCoins(sdk.NewInt64Coin("utia", 1900), sdk.NewInt64Coin("stake", 700)))
	supply := testApp.BankKeeper.GetSupply(ctx, "stake")
	accountNumber := testApp.AccountKeeper.GetModuleAccount(ctx, fibretypes.ModuleName).GetAccountNumber()

	require.NoError(t, applyV11FromFibreV1(t, testApp, ctx))
	account, found := k.GetEscrowAccount(ctx, lower)
	require.True(t, found)
	require.Equal(t, sdk.NewInt64Coin("utia", 1500), account.Balance)
	require.Equal(t, sdk.NewInt64Coin("utia", 900), account.AvailableBalance)
	_, found = k.GetEscrowAccount(ctx, upper)
	require.False(t, found)
	_, found = k.GetEscrowAccount(ctx, strings.ToUpper(other.String()))
	require.False(t, found)
	require.Equal(t, sdk.NewInt64Coin("stake", 700), testApp.BankKeeper.GetBalance(ctx, other, "stake"))
	require.Equal(t, supply, testApp.BankKeeper.GetSupply(ctx, "stake"))
	require.Equal(t, accountNumber, testApp.AccountKeeper.GetModuleAccount(ctx, fibretypes.ModuleName).GetAccountNumber())
	versions, err := testApp.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, versions[fibretypes.ModuleName])
	merged, found := k.GetWithdrawal(ctx, lower, requested)
	require.True(t, found)
	require.Equal(t, sdk.NewInt64Coin("utia", 500), merged.Amount)
	require.Equal(t, withdrawals[1].AvailableTimestamp, merged.AvailableTimestamp)
	require.Len(t, k.GetWithdrawalsBySigner(ctx, lower), 2)
	require.Empty(t, k.GetWithdrawalsBySigner(ctx, upper))
	require.Empty(t, k.GetWithdrawalsBySigner(ctx, strings.ToUpper(other.String())))
	exported := k.ExportGenesis(ctx)
	require.Len(t, exported.Withdrawals, 2)
	require.NoError(t, exported.Validate())

	// Repeating the migration cannot refund twice or duplicate withdrawals.
	require.NoError(t, applyV11FromFibreV1(t, testApp, ctx))
	require.Equal(t, exported, k.ExportGenesis(ctx))
	require.Equal(t, sdk.NewInt64Coin("stake", 700), testApp.BankKeeper.GetBalance(ctx, other, "stake"))
	// Exercise the availability index: the colliding requests must not pay early.
	require.NoError(t, k.BeginBlocker(ctx.WithBlockTime(ctx.BlockTime().Add(time.Hour))))
	require.True(t, testApp.BankKeeper.GetBalance(ctx, owner, "utia").IsZero())
	require.NoError(t, k.BeginBlocker(ctx.WithBlockTime(ctx.BlockTime().Add(2*time.Hour))))
	require.Equal(t, sdk.NewInt64Coin("utia", 500), testApp.BankKeeper.GetBalance(ctx, owner, "utia"))
	require.NoError(t, k.BeginBlocker(ctx.WithBlockTime(ctx.BlockTime().Add(3*time.Hour))))
	require.Equal(t, sdk.NewInt64Coin("utia", 600), testApp.BankKeeper.GetBalance(ctx, owner, "utia"))
	account, found = k.GetEscrowAccount(ctx, lower)
	require.True(t, found)
	require.Equal(t, account.AvailableBalance, account.Balance)
	iterator := k.GetWithdrawalsByAvailableIterator(ctx, ctx.BlockTime().Add(24*time.Hour))
	defer iterator.Close()
	require.False(t, iterator.Valid(), "no stale availability entries may pay a withdrawal twice")
}

func TestV11FibreMigrationRollsBackRefunds(t *testing.T) {
	testApp, _, _ := util.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(false).WithBlockHeight(1).WithBlockTime(util.GenesisTime.Add(time.Hour))
	owners := []sdk.AccAddress{bytes.Repeat([]byte{1}, 20), bytes.Repeat([]byte{2}, 20)}
	for _, owner := range owners {
		testApp.FibreKeeper.SetEscrowAccount(ctx, fibretypes.EscrowAccount{
			Signer: owner.String(), Balance: sdk.NewInt64Coin("stake", 50), AvailableBalance: sdk.NewInt64Coin("stake", 40),
		})
		testApp.FibreKeeper.SetWithdrawal(ctx, fibretypes.Withdrawal{
			Signer: owner.String(), Amount: sdk.NewInt64Coin("stake", 10),
			RequestedTimestamp: ctx.BlockTime(), AvailableTimestamp: ctx.BlockTime().Add(time.Hour),
		})
	}
	// The second refund fails after the first one has succeeded in the cache.
	fundFibreModule(t, testApp, ctx, sdk.NewCoins(sdk.NewInt64Coin("stake", 50)))
	before := testApp.FibreKeeper.ExportGenesis(ctx)
	require.Error(t, applyV11FromFibreV1(t, testApp, ctx))
	require.Equal(t, before, testApp.FibreKeeper.ExportGenesis(ctx))
	for _, owner := range owners {
		require.True(t, testApp.BankKeeper.GetBalance(ctx, owner, "stake").IsZero())
	}
	fundFibreModule(t, testApp, ctx, sdk.NewCoins(sdk.NewInt64Coin("stake", 50)))
	require.NoError(t, applyV11FromFibreV1(t, testApp, ctx))
	for _, owner := range owners {
		require.Equal(t, sdk.NewInt64Coin("stake", 50), testApp.BankKeeper.GetBalance(ctx, owner, "stake"))
	}
}
