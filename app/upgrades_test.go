package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/test/util"
	"github.com/celestiaorg/celestia-app/v8/test/util/testfactory"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgrades(t *testing.T) {
	t.Run("app.New() should register a v8 upgrade handler", func(t *testing.T) {
		logger := log.NewNopLogger()
		db := tmdb.NewMemDB()
		traceStore := &NoopWriter{}
		timeoutCommit := time.Second
		appOptions := NoopAppOptions{}

		testApp := app.New(logger, db, traceStore, timeoutCommit, appOptions, baseapp.SetChainID(testfactory.ChainID))

		require.False(t, testApp.UpgradeKeeper.HasHandler("v7"))
		require.True(t, testApp.UpgradeKeeper.HasHandler("v8"))
	})
}

func TestApplyUpgrade(t *testing.T) {
	t.Run("v6 to v8 upgrade should set the min commission rate to 20%", func(t *testing.T) {
		consensusParams := app.DefaultConsensusParams()
		consensusParams.Version.App = 6
		testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)
		require.True(t, testApp.UpgradeKeeper.HasHandler("v8"))

		ctx := testApp.NewContext(false)
		oldMinCommissionRate, err := math.LegacyNewDecFromStr("0.10")
		require.NoError(t, err)
		err = testApp.StakingKeeper.SetParams(ctx, stakingtypes.Params{
			MinCommissionRate: oldMinCommissionRate,
		})
		require.NoError(t, err)

		plan := upgradetypes.Plan{
			Name:   "v8",
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
	t.Run("v7 to v8 upgrade should skip commission rate migration", func(t *testing.T) {
		consensusParams := app.DefaultConsensusParams()
		consensusParams.Version.App = 7
		testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)
		require.True(t, testApp.UpgradeKeeper.HasHandler("v8"))

		ctx := testApp.NewContext(false).WithBlockHeader(tmproto.Header{Version: tmversion.Consensus{App: 7}})
		oldMinCommissionRate, err := math.LegacyNewDecFromStr("0.10")
		require.NoError(t, err)
		err = testApp.StakingKeeper.SetParams(ctx, stakingtypes.Params{
			MinCommissionRate: oldMinCommissionRate,
		})
		require.NoError(t, err)

		plan := upgradetypes.Plan{
			Name:   "v8",
			Time:   time.Now(),
			Height: 1,
			Info:   "info",
		}
		err = testApp.UpgradeKeeper.ApplyUpgrade(ctx, plan)
		require.NoError(t, err)

		// Commission rate should remain 10% — v7 already applied this migration.
		ctx = testApp.NewContext(false)
		got, err := testApp.StakingKeeper.GetParams(ctx)
		require.NoError(t, err)
		require.Equal(t, oldMinCommissionRate, got.MinCommissionRate)
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
			name:           "should increase rate to 20%",
			initialRate:    "0.10",
			initialMaxRate: "0.60",
			wantRate:       "0.20",
			wantMaxRate:    "0.60",
			shouldUpdate:   true,
		},
		{
			name:           "should increase max rate to 60%",
			initialRate:    "0.20",
			initialMaxRate: "0.30",
			wantRate:       "0.20",
			wantMaxRate:    "0.60",
			shouldUpdate:   true,
		},
		{
			name:           "should increase both",
			initialRate:    "0.05",
			initialMaxRate: "0.08",
			wantRate:       "0.20",
			wantMaxRate:    "0.60",
			shouldUpdate:   true,
		},
		{
			name:           "should increase both if both at 0",
			initialRate:    "0.00",
			initialMaxRate: "0.00",
			wantRate:       "0.20",
			wantMaxRate:    "0.60",
			shouldUpdate:   true,
		},
		{
			name:           "should not update if both are already at 20% and 60%",
			initialRate:    "0.20",
			initialMaxRate: "0.60",
			wantRate:       "0.20",
			wantMaxRate:    "0.60",
			shouldUpdate:   false,
		},
		{
			name:           "should not update if both are above 20% and 60%",
			initialRate:    "0.25",
			initialMaxRate: "0.80",
			wantRate:       "0.25",
			wantMaxRate:    "0.80",
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
