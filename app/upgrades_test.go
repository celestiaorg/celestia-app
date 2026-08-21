package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

func TestUpgrades(t *testing.T) {
	t.Run("app.New() should register a v10 upgrade handler", func(t *testing.T) {
		logger := log.NewNopLogger()
		db := tmdb.NewMemDB()
		traceStore := &NoopWriter{}
		delayedPrecommitTimeout := time.Second
		appOptions := NoopAppOptions{}

		testApp := app.New(logger, db, traceStore, delayedPrecommitTimeout, 0, appOptions, baseapp.SetChainID(testfactory.ChainID))

		require.False(t, testApp.UpgradeKeeper.HasHandler("v9"))
		require.True(t, testApp.UpgradeKeeper.HasHandler("v10"))
	})
}

func TestV10UpgradeConvertsPrefundedFibreBaseAccount(t *testing.T) {
	funder := testfactory.GenerateAccounts(1)[0]
	testApp, keyring := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), funder)
	ctx := testApp.NewUncachedContext(false, cmtproto.Header{Height: testApp.LastBlockHeight() + 1})

	fibreAddress := testApp.AccountKeeper.GetModuleAddress(fibretypes.ModuleName)

	// Simulate v9 state: a bank send to Fibre's future address created a
	// BaseAccount and deposited a small balance before Fibre was enabled.
	baseAccount := testApp.AccountKeeper.NewAccountWithAddress(ctx, fibreAddress)
	testApp.AccountKeeper.SetAccount(ctx, baseAccount)
	prefund := sdk.NewInt64Coin(appconsts.BondDenom, 1)
	funderAddress := testfactory.GetAddress(keyring, funder)
	require.NoError(t, testApp.BankKeeper.SendCoins(ctx, funderAddress, fibreAddress, sdk.NewCoins(prefund)))
	require.IsType(t, &authtypes.BaseAccount{}, testApp.AccountKeeper.GetAccount(ctx, fibreAddress))

	applyV10Upgrade(t, testApp, ctx)

	convertedAccount := testApp.AccountKeeper.GetAccount(ctx, fibreAddress)
	convertedModuleAccount, ok := convertedAccount.(sdk.ModuleAccountI)
	require.True(t, ok)
	require.Equal(t, fibretypes.ModuleName, convertedModuleAccount.GetName())
	require.Equal(t, baseAccount.GetAccountNumber(), convertedModuleAccount.GetAccountNumber())
	require.Equal(t, prefund, testApp.BankKeeper.GetBalance(ctx, fibreAddress, appconsts.BondDenom))
}

func TestV10UpgradeCreatesMissingFibreModuleAccount(t *testing.T) {
	testApp, _, _ := util.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	ctx := testApp.NewUncachedContext(false, cmtproto.Header{Height: 1})

	fibreAddress := testApp.AccountKeeper.GetModuleAddress(fibretypes.ModuleName)
	require.Nil(t, testApp.AccountKeeper.GetAccount(ctx, fibreAddress))

	applyV10Upgrade(t, testApp, ctx)

	moduleAccount, ok := testApp.AccountKeeper.GetAccount(ctx, fibreAddress).(sdk.ModuleAccountI)
	require.True(t, ok)
	require.Equal(t, fibretypes.ModuleName, moduleAccount.GetName())
}

func applyV10Upgrade(t *testing.T, testApp *app.App, ctx sdk.Context) {
	t.Helper()

	require.NoError(t, testApp.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{
		Name:   "v10",
		Height: ctx.BlockHeight(),
	}))
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
