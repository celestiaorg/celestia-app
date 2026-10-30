package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

// TestV11UpgradePreservesState checks that the v11 upgrade rescales the
// block-count parameters for 0.8s blocks, runs module migrations and doesn't
// repeat the v10 upgrade's state changes.
func TestV11UpgradePreservesState(t *testing.T) {
	testApp, _, _ := util.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(false).WithBlockHeight(1)
	seedV10BlockCountParams(t, testApp, ctx)

	fibreAddress := testApp.AccountKeeper.GetModuleAddress(fibretypes.ModuleName)
	fibreAccount := testApp.AccountKeeper.GetModuleAccount(ctx, fibretypes.ModuleName)
	consensusParams, err := testApp.ConsensusKeeper.ParamsStore.Get(ctx)
	require.NoError(t, err)
	slashingParams, err := testApp.SlashingKeeper.GetParams(ctx)
	require.NoError(t, err)
	stakingParams, err := testApp.StakingKeeper.GetParams(ctx)
	require.NoError(t, err)
	fibreParams := testApp.FibreKeeper.GetParams(ctx)
	moduleVersions, err := testApp.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.NoError(t, err)

	require.NoError(t, testApp.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{
		Name:   "v11",
		Height: ctx.BlockHeight(),
	}))

	require.Equal(t, fibreAccount.GetAccountNumber(), testApp.AccountKeeper.GetAccount(ctx, fibreAddress).GetAccountNumber())
	gotModuleVersions, err := testApp.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.NoError(t, err)
	require.Equal(t, moduleVersions, gotModuleVersions)

	// Each rescaled parameter is v10 x3.25; nothing else in its params changes.
	consensusParams.Evidence.MaxAgeNumBlocks = 1_314_300
	gotConsensusParams, err := testApp.ConsensusKeeper.ParamsStore.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, consensusParams, gotConsensusParams)
	slashingParams.SignedBlocksWindow = 32_500
	gotSlashingParams, err := testApp.SlashingKeeper.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, slashingParams, gotSlashingParams)
	stakingParams.HistoricalEntries = 32_500
	gotStakingParams, err := testApp.StakingKeeper.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, stakingParams, gotStakingParams)
	fibreParams.PaymentPromiseHeightWindow = 3_250
	require.Equal(t, fibreParams, testApp.FibreKeeper.GetParams(ctx))
	require.Equal(t, uint64((4 * time.Second).Nanoseconds()), testApp.IBCKeeper.ConnectionKeeper.GetParams(ctx).MaxExpectedTimePerBlock)
}

// seedV10BlockCountParams sets the mainnet v10 values of the parameters the
// v11 upgrade rescales.
func seedV10BlockCountParams(t *testing.T, testApp *app.App, ctx sdk.Context) {
	t.Helper()
	consensusParams, err := testApp.ConsensusKeeper.ParamsStore.Get(ctx)
	require.NoError(t, err)
	consensusParams.Evidence.MaxAgeNumBlocks = 404_400
	require.NoError(t, testApp.ConsensusKeeper.ParamsStore.Set(ctx, consensusParams))

	slashingParams, err := testApp.SlashingKeeper.GetParams(ctx)
	require.NoError(t, err)
	slashingParams.SignedBlocksWindow = 10_000
	require.NoError(t, testApp.SlashingKeeper.SetParams(ctx, slashingParams))

	stakingParams, err := testApp.StakingKeeper.GetParams(ctx)
	require.NoError(t, err)
	stakingParams.HistoricalEntries = 10_000
	require.NoError(t, testApp.StakingKeeper.SetParams(ctx, stakingParams))

	fibreParams := testApp.FibreKeeper.GetParams(ctx)
	fibreParams.PaymentPromiseHeightWindow = 1_000
	testApp.FibreKeeper.SetParams(ctx, fibreParams)

	connectionParams := testApp.IBCKeeper.ConnectionKeeper.GetParams(ctx)
	connectionParams.MaxExpectedTimePerBlock = uint64((13 * time.Second).Nanoseconds())
	testApp.IBCKeeper.ConnectionKeeper.SetParams(ctx, connectionParams)
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
