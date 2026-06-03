package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v9/test/util"
	"github.com/celestiaorg/celestia-app/v9/test/util/testfactory"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgrades(t *testing.T) {
	t.Run("app.New() should register a v9 upgrade handler", func(t *testing.T) {
		logger := log.NewNopLogger()
		db := tmdb.NewMemDB()
		traceStore := &NoopWriter{}
		delayedPrecommitTimeout := time.Second
		appOptions := NoopAppOptions{}

		testApp := app.New(logger, db, traceStore, delayedPrecommitTimeout, 0, appOptions, baseapp.SetChainID(testfactory.ChainID))

		require.False(t, testApp.UpgradeKeeper.HasHandler("v8"))
		require.True(t, testApp.UpgradeKeeper.HasHandler("v9"))
	})
}

func TestSetMaxExpectedTimePerBlock(t *testing.T) {
	consensusParams := app.DefaultConsensusParams()
	testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)
	ctx := testApp.NewContext(false)

	err := testApp.SetMaxExpectedTimePerBlock(ctx)
	require.NoError(t, err)

	got := testApp.IBCKeeper.ConnectionKeeper.GetParams(ctx)
	want := uint64((13 * time.Second).Nanoseconds())
	assert.Equal(t, want, got.MaxExpectedTimePerBlock)
}

func TestApplyUpgradeSetBlockMaxBytes(t *testing.T) {
	t.Run("apply upgrade should set Block.MaxBytes to 32 MiB", func(t *testing.T) {
		consensusParams := app.DefaultConsensusParams()
		testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)
		require.True(t, testApp.UpgradeKeeper.HasHandler("v9"))

		ctx := testApp.NewContext(false)

		// Manually set MaxBytes to 128 MiB via the params store because
		// NewTestAppWithGenesisSet overrides MaxBytes to BlockMaxBytes.
		oldMaxBytes := int64(128 * 1024 * 1024) // 128 MiB
		params, err := testApp.ConsensusKeeper.ParamsStore.Get(ctx)
		require.NoError(t, err)
		params.Block.MaxBytes = oldMaxBytes
		err = testApp.ConsensusKeeper.ParamsStore.Set(ctx, params)
		require.NoError(t, err)

		// Verify the initial value is 128 MiB.
		params, err = testApp.ConsensusKeeper.ParamsStore.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, oldMaxBytes, params.Block.MaxBytes)

		// Apply the upgrade.
		plan := upgradetypes.Plan{
			Name:   "v9",
			Height: 1,
			Info:   "test",
		}
		err = testApp.UpgradeKeeper.ApplyUpgrade(ctx, plan)
		require.NoError(t, err)

		// Verify Block.MaxBytes was updated to 32 MiB.
		params, err = testApp.ConsensusKeeper.ParamsStore.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, int64(appconsts.BlockMaxBytes), params.Block.MaxBytes)
	})
}

func TestApplyUpgradeSetEvidenceMaxAgeNumBlocks(t *testing.T) {
	t.Run("apply upgrade should set Evidence.MaxAgeNumBlocks to 559940", func(t *testing.T) {
		consensusParams := app.DefaultConsensusParams()
		testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)
		require.True(t, testApp.UpgradeKeeper.HasHandler("v9"))

		ctx := testApp.NewContext(false)

		// Manually set MaxAgeNumBlocks to the pre-v9 mainnet value (242,640)
		// because NewTestAppWithGenesisSet uses DefaultConsensusParams which
		// already has the v9 value.
		oldMaxAgeNumBlocks := int64(242_640)
		params, err := testApp.ConsensusKeeper.ParamsStore.Get(ctx)
		require.NoError(t, err)
		params.Evidence.MaxAgeNumBlocks = oldMaxAgeNumBlocks
		err = testApp.ConsensusKeeper.ParamsStore.Set(ctx, params)
		require.NoError(t, err)

		// Verify the initial value is 242,640.
		params, err = testApp.ConsensusKeeper.ParamsStore.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, oldMaxAgeNumBlocks, params.Evidence.MaxAgeNumBlocks)

		// Apply the upgrade.
		plan := upgradetypes.Plan{
			Name:   "v9",
			Height: 1,
			Info:   "test",
		}
		err = testApp.UpgradeKeeper.ApplyUpgrade(ctx, plan)
		require.NoError(t, err)

		// Verify Evidence.MaxAgeNumBlocks was updated to 559,940.
		params, err = testApp.ConsensusKeeper.ParamsStore.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, int64(appconsts.MaxAgeNumBlocks), params.Evidence.MaxAgeNumBlocks)
	})
}

func TestApplyUpgradeSetGovMaxSquareSize(t *testing.T) {
	t.Run("apply upgrade should set blob GovMaxSquareSize to 256", func(t *testing.T) {
		consensusParams := app.DefaultConsensusParams()
		testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)
		require.True(t, testApp.UpgradeKeeper.HasHandler("v9"))

		ctx := testApp.NewContext(false)

		// Manually set GovMaxSquareSize to the Mocha value - 512.
		oldGovMaxSquareSize := uint64(512)
		params := testApp.BlobKeeper.GetParams(ctx)
		params.GovMaxSquareSize = oldGovMaxSquareSize
		testApp.BlobKeeper.SetParams(ctx, params)

		// Verify the initial value is 512.
		require.Equal(t, oldGovMaxSquareSize, testApp.BlobKeeper.GetParams(ctx).GovMaxSquareSize)

		// Apply the upgrade.
		plan := upgradetypes.Plan{
			Name:   "v9",
			Height: 1,
			Info:   "test",
		}
		err := testApp.UpgradeKeeper.ApplyUpgrade(ctx, plan)
		require.NoError(t, err)

		// Verify GovMaxSquareSize was updated to 256.
		require.Equal(t, appconsts.MaxSquareSize, testApp.BlobKeeper.GetParams(ctx).GovMaxSquareSize)
	})
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
