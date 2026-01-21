package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v7/app"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/test/util"
	"github.com/celestiaorg/celestia-app/v7/test/util/testfactory"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

func TestUpgrades(t *testing.T) {
	t.Run("app.New() should register a v7 upgrade handler", func(t *testing.T) {
		logger := log.NewNopLogger()
		db := tmdb.NewMemDB()
		traceStore := &NoopWriter{}
		timeoutCommit := time.Second
		appOptions := NoopAppOptions{}

		testApp := app.New(logger, db, traceStore, timeoutCommit, appOptions, baseapp.SetChainID(testfactory.ChainID))

		require.False(t, testApp.UpgradeKeeper.HasHandler("v6"))
		require.True(t, testApp.UpgradeKeeper.HasHandler("v7"))
	})
}

func TestApplyUpgrade(t *testing.T) {
	t.Run("apply upgrade should set the min commission rate to 20%", func(t *testing.T) {
		consensusParams := app.DefaultConsensusParams()
		consensusParams.Version.App = 5
		testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)
		require.True(t, testApp.UpgradeKeeper.HasHandler("v7"))

		ctx := testApp.NewContext(false)
		oldMinCommissionRate, err := math.LegacyNewDecFromStr("0.10")
		require.NoError(t, err)
		// Set the min commission rate to 10% because that is what v6 set it to.
		err = testApp.StakingKeeper.SetParams(ctx, stakingtypes.Params{
			MinCommissionRate: oldMinCommissionRate,
		})
		require.NoError(t, err)
		params, err := testApp.StakingKeeper.GetParams(ctx)
		require.NoError(t, err)
		require.Equal(t, oldMinCommissionRate, params.MinCommissionRate)

		// Apply the upgrade.
		plan := upgradetypes.Plan{
			Name:   "v7",
			Time:   time.Now(),
			Height: 1,
			Info:   "info",
		}
		err = testApp.UpgradeKeeper.ApplyUpgrade(ctx, plan)
		require.NoError(t, err)

		ctx = testApp.NewContext(false)
		got, err := testApp.StakingKeeper.GetParams(ctx)
		require.NoError(t, err)
		require.Equal(t, appconsts.MinCommissionRate, got.MinCommissionRate)
	})
	t.Run("apply upgrade should set the commission rate for a validator to 20% if it was less than that", func(t *testing.T) {
		consensusParams := app.DefaultConsensusParams()
		consensusParams.Version.App = 5
		testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)
		require.True(t, testApp.UpgradeKeeper.HasHandler("v7"))

		ctx := testApp.NewContext(false)
		validators, err := testApp.StakingKeeper.GetAllValidators(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, len(validators))
		validator := validators[0]
		oldMinCommissionRate, err := math.LegacyNewDecFromStr("0.05")
		require.NoError(t, err)
		require.Equal(t, oldMinCommissionRate, validator.Commission.Rate)

		// Apply the upgrade.
		plan := upgradetypes.Plan{
			Name:   "v7",
			Time:   time.Now(),
			Height: 1,
			Info:   "info",
		}
		// Set the block time to 25 hours ahead of the genesis block to ensure
		// the commission rate can be updated. If the block time is within 24
		// hours of the genesis block, the commission rate will fail to update
		// due to ErrCommissionUpdateTime.
		ctx = testApp.NewContext(false).WithBlockTime(util.GenesisTime.Add(time.Hour * 25))
		err = testApp.UpgradeKeeper.ApplyUpgrade(ctx, plan)
		require.NoError(t, err)

		ctx = testApp.NewContext(false)
		validators, err = testApp.StakingKeeper.GetAllValidators(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, len(validators))
		validator = validators[0]
		require.Equal(t, appconsts.MinCommissionRate, validator.Commission.Rate)
	})
}

func TestUpdateValidatorCommissionRates(t *testing.T) {
	testCases := []struct {
		name           string
		initialRate    string
		initialMaxRate string
		wantRate       string
		wantMaxRate    string
		shouldUpdate   bool
	}{
		{
			name:           "should increase rate to min commission rate and max rate to max commission rate",
			initialRate:    "0.05", // 5%
			initialMaxRate: "0.08", // 8%
			wantRate:       "0.20", // 20% (MinCommissionRate)
			wantMaxRate:    "0.60", // 60% (MaxCommissionRate)
			shouldUpdate:   true,
		},
		{
			name:           "should increase rate to min commission rate and max rate to max commission rate when max rate is below 60%",
			initialRate:    "0.03", // 3%
			initialMaxRate: "0.25", // 25%
			wantRate:       "0.20", // 20% (MinCommissionRate)
			wantMaxRate:    "0.60", // 60% (MaxCommissionRate)
			shouldUpdate:   true,
		},
		{
			name:           "should increase max rate to max commission rate when rate is compliant but max rate is below 60%",
			initialRate:    "0.25", // 25%
			initialMaxRate: "0.30", // 30%
			wantRate:       "0.25", // unchanged
			wantMaxRate:    "0.60", // 60% (MaxCommissionRate)
			shouldUpdate:   true,
		},
		{
			name:           "should increase max rate to max commission rate when rate is at minimum but max rate is below 60%",
			initialRate:    "0.20", // 20% (exactly minimum)
			initialMaxRate: "0.20", // 20%
			wantRate:       "0.20", // unchanged
			wantMaxRate:    "0.60", // 60% (MaxCommissionRate)
			shouldUpdate:   true,
		},
		{
			name:           "zero commission rate - should be updated to minimum and max rate to max commission rate",
			initialRate:    "0.00", // 0%
			initialMaxRate: "0.05", // 5%
			wantRate:       "0.20", // 20% (MinCommissionRate)
			wantMaxRate:    "0.60", // 60% (MaxCommissionRate)
			shouldUpdate:   true,
		},
		{
			name:           "should increase max rate to max commission rate when max rate is 50%",
			initialRate:    "0.20", // 20%
			initialMaxRate: "0.50", // 50%
			wantRate:       "0.20", // unchanged
			wantMaxRate:    "0.60", // 60% (MaxCommissionRate)
			shouldUpdate:   true,
		},
		{
			name:           "should not update if max rate is exactly 60%",
			initialRate:    "0.20", // 20%
			initialMaxRate: "0.60", // 60% (exactly MaxCommissionRate)
			wantRate:       "0.20", // unchanged
			wantMaxRate:    "0.60", // unchanged
			shouldUpdate:   false,
		},
		{
			name:           "should not update if max rate is above 60%",
			initialRate:    "0.25", // 25%
			initialMaxRate: "0.80", // 80%
			wantRate:       "0.25", // unchanged
			wantMaxRate:    "0.80", // unchanged
			shouldUpdate:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			consensusParams := app.DefaultConsensusParams()
			testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)

			ctx := testApp.NewContext(false).WithBlockTime(util.GenesisTime.Add(time.Hour * 25))

			validator := createValidatorWithCommission(t, testApp, ctx, tc.initialRate, tc.initialMaxRate)

			valAddr, err := sdk.ValAddressFromBech32(validator.GetOperator())
			require.NoError(t, err)
			validatorBefore, err := testApp.StakingKeeper.GetValidator(ctx, valAddr)
			require.NoError(t, err)

			assertCommissionRates(t, validatorBefore, tc.initialRate, tc.initialMaxRate)

			err = testApp.UpdateValidatorCommissionRates(ctx)
			require.NoError(t, err)

			validatorAfter, err := testApp.StakingKeeper.GetValidator(ctx, valAddr)
			require.NoError(t, err)

			assertCommissionRates(t, validatorAfter, tc.wantRate, tc.wantMaxRate)

			if tc.shouldUpdate {
				require.Equal(t, ctx.BlockTime(), validatorAfter.Commission.UpdateTime, "UpdateTime should be set to current block time")
			}
		})
	}
}

// createValidatorWithCommission creates a validator with specific commission
// rates for testing
func createValidatorWithCommission(t *testing.T, testApp *app.App, ctx sdk.Context, rate string, maxRate string) stakingtypes.Validator {
	rateDec, err := math.LegacyNewDecFromStr(rate)
	require.NoError(t, err)

	maxRateDec, err := math.LegacyNewDecFromStr(maxRate)
	require.NoError(t, err)

	maxChangeRateDec, err := math.LegacyNewDecFromStr("0.10") // 10%
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

// assertCommissionRates verifies that a validator has the expected commission rates
func assertCommissionRates(t *testing.T, validator stakingtypes.Validator, expectedRate string, expectedMaxRate string) {
	wantRate, err := math.LegacyNewDecFromStr(expectedRate)
	require.NoError(t, err)
	wantMaxRate, err := math.LegacyNewDecFromStr(expectedMaxRate)
	require.NoError(t, err)

	require.Equal(t, wantRate, validator.Commission.Rate)
	require.Equal(t, wantMaxRate, validator.Commission.MaxRate)
}

func TestMaxCommissionRate(t *testing.T) {
	t.Run("editing validator commission to 55% should succeed", func(t *testing.T) {
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
