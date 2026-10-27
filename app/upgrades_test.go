package app_test

import (
	"strings"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

func TestUpgrades(t *testing.T) {
	t.Run("app.New() should register a v11 upgrade handler", func(t *testing.T) {
		logger := log.NewNopLogger()
		db := tmdb.NewMemDB()
		traceStore := &NoopWriter{}
		delayedPrecommitTimeout := time.Second
		appOptions := NoopAppOptions{}

		testApp := app.New(logger, db, traceStore, delayedPrecommitTimeout, 0, appOptions, baseapp.SetChainID(testfactory.ChainID))

		require.False(t, testApp.UpgradeKeeper.HasHandler("v10"))
		require.True(t, testApp.UpgradeKeeper.HasHandler("v11"))
	})
}

// TestV11UpgradePreservesState checks that the v11 upgrade only runs module
// migrations and doesn't repeat the v10 upgrade's state changes.
func TestV11UpgradePreservesState(t *testing.T) {
	testApp, _, _ := util.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(false).WithBlockHeight(1)

	fibreAddress := testApp.AccountKeeper.GetModuleAddress(fibretypes.ModuleName)
	fibreAccount := testApp.AccountKeeper.GetModuleAccount(ctx, fibretypes.ModuleName)
	consensusParams, err := testApp.ConsensusKeeper.ParamsStore.Get(ctx)
	require.NoError(t, err)
	moduleVersions, err := testApp.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.NoError(t, err)

	require.NoError(t, testApp.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{
		Name:   "v11",
		Height: ctx.BlockHeight(),
	}))

	require.Equal(t, fibreAccount.GetAccountNumber(), testApp.AccountKeeper.GetAccount(ctx, fibreAddress).GetAccountNumber())
	gotConsensusParams, err := testApp.ConsensusKeeper.ParamsStore.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, consensusParams, gotConsensusParams)
	gotModuleVersions, err := testApp.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.NoError(t, err)
	require.Equal(t, moduleVersions, gotModuleVersions)
}

// TestV11UpgradeMigratesFibreEscrow checks that the v11 upgrade runs the fibre
// 1->2 store migration when the chain comes from fibre consensus version 1, as
// a v10 chain does: an escrow record keyed by an uppercase signer is merged
// into the canonical record and a non-utia escrow record is refunded.
func TestV11UpgradeMigratesFibreEscrow(t *testing.T) {
	testApp, _, _ := util.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(false).WithBlockHeight(1)

	moduleVersions, err := testApp.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), moduleVersions[fibretypes.ModuleName])
	moduleVersions[fibretypes.ModuleName] = 1
	require.NoError(t, testApp.UpgradeKeeper.SetModuleVersionMap(ctx, moduleVersions))

	upperSigner := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	upper := strings.ToUpper(upperSigner.String())
	utia := sdk.NewInt64Coin(appconsts.BondDenom, 1000)
	testApp.FibreKeeper.SetEscrowAccount(ctx, fibretypes.EscrowAccount{Signer: upper, Balance: utia, AvailableBalance: utia})

	stuckSigner := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	stuck := sdk.NewInt64Coin("uatom", 500)
	testApp.FibreKeeper.SetEscrowAccount(ctx, fibretypes.EscrowAccount{Signer: stuckSigner.String(), Balance: stuck, AvailableBalance: stuck})

	// Fund the module account with what the two escrow records say it holds.
	custody := sdk.NewCoins(utia, stuck)
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, custody))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, fibretypes.ModuleName, custody))

	require.NoError(t, testApp.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{
		Name:   "v11",
		Height: ctx.BlockHeight(),
	}))

	gotModuleVersions, err := testApp.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), gotModuleVersions[fibretypes.ModuleName])

	_, found := testApp.FibreKeeper.GetEscrowAccount(ctx, upper)
	require.False(t, found)
	merged, found := testApp.FibreKeeper.GetEscrowAccount(ctx, upperSigner.String())
	require.True(t, found)
	require.Equal(t, fibretypes.EscrowAccount{Signer: upperSigner.String(), Balance: utia, AvailableBalance: utia}, merged)

	_, found = testApp.FibreKeeper.GetEscrowAccount(ctx, stuckSigner.String())
	require.False(t, found)
	require.Equal(t, stuck, testApp.BankKeeper.GetBalance(ctx, stuckSigner, stuck.Denom))
	fibreAddress := testApp.AccountKeeper.GetModuleAddress(fibretypes.ModuleName)
	require.Equal(t, sdk.NewCoins(utia), testApp.BankKeeper.GetAllBalances(ctx, fibreAddress))
}

// createValidatorWithCommission creates a validator with specific commission
// rates for testing
func createValidatorWithCommission(t *testing.T, testApp *app.App, ctx sdk.Context, rate string, maxRate string) stakingtypes.Validator {
	rateDec, err := math.LegacyNewDecFromStr(rate)
	require.NoError(t, err)

	maxRateDec, err := math.LegacyNewDecFromStr(maxRate)
	require.NoError(t, err)

	maxChangeRateDec := math.LegacyOneDec()
	require.NoError(t, err)

	validators, err := testApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	require.Greater(t, len(validators), 0, "Should have at least one validator")

	validator := validators[0]
	validator.Commission = stakingtypes.NewCommission(rateDec, maxRateDec, maxChangeRateDec)

	err = testApp.StakingKeeper.SetValidator(ctx, validator)
	require.NoError(t, err)

	return validator
}

func TestMaxCommissionRate(t *testing.T) {
	t.Run("editing validator commission to 55% should succeed", func(t *testing.T) {
		consensusParams := app.DefaultConsensusParams()
		testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)

		// Set the block time to 25 hours ahead of the genesis block to ensure
		// the commission rate can be updated.
		ctx := testApp.NewContext(false).WithBlockTime(util.GenesisTime.Add(time.Hour * 25))

		validator := createValidatorWithCommission(t, testApp, ctx, "0.20", "1.00")
		valAddr, err := sdk.ValAddressFromBech32(validator.GetOperator())
		require.NoError(t, err)

		msgServer := stakingkeeper.NewMsgServerImpl(testApp.StakingKeeper)
		newRate := math.LegacyNewDecWithPrec(55, 2) // 55%
		description := stakingtypes.NewDescription("moniker", "identity", "website", "securityContact", "details")
		msg := stakingtypes.NewMsgEditValidator(
			valAddr.String(),
			description,
			&newRate,
			nil,
		)

		_, err = msgServer.EditValidator(ctx, msg)
		require.NoError(t, err)

		// Verify the commission rate was updated
		updatedValidator, err := testApp.StakingKeeper.GetValidator(ctx, valAddr)
		require.NoError(t, err)
		require.Equal(t, newRate, updatedValidator.Commission.Rate)
	})

	t.Run("editing validator commission to 65% should fail", func(t *testing.T) {
		consensusParams := app.DefaultConsensusParams()
		testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)

		// Set the block time to 25 hours ahead of the genesis block to ensure
		// the commission rate can be updated.
		ctx := testApp.NewContext(false).WithBlockTime(util.GenesisTime.Add(time.Hour * 25))

		// Set up validator with a high max change rate to allow commission changes
		validator := createValidatorWithCommission(t, testApp, ctx, "0.20", "1.00")
		valAddr, err := sdk.ValAddressFromBech32(validator.GetOperator())
		require.NoError(t, err)

		msgServer := stakingkeeper.NewMsgServerImpl(testApp.StakingKeeper)
		newRate := math.LegacyNewDecWithPrec(65, 2) // 65%
		description := stakingtypes.NewDescription("moniker", "identity", "website", "securityContact", "details")
		msg := stakingtypes.NewMsgEditValidator(
			valAddr.String(),
			description,
			&newRate,
			nil,
		)

		_, err = msgServer.EditValidator(ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "commission rate cannot be greater than the max commission rate")
	})
}
