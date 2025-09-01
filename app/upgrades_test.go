package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/test/util"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	"github.com/stretchr/testify/require"
)

func TestUpgrades(t *testing.T) {
	t.Run("app.New() should register a v6 upgrade handler", func(t *testing.T) {
		logger := log.NewNopLogger()
		db := tmdb.NewMemDB()
		traceStore := &NoopWriter{}
		timeoutCommit := time.Second
		appOptions := NoopAppOptions{}

		testApp := app.New(logger, db, traceStore, timeoutCommit, appOptions, baseapp.SetChainID(testfactory.ChainID))

		require.False(t, testApp.UpgradeKeeper.HasHandler("v5"))
		require.True(t, testApp.UpgradeKeeper.HasHandler("v6"))
	})
}

func TestApplyUpgrade(t *testing.T) {
	t.Run("apply upgrade should set ICA host params to an explicit allowlist of messages", func(t *testing.T) {
		consensusParams := app.DefaultConsensusParams()
		consensusParams.Version.App = 5
		testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)
		require.True(t, testApp.UpgradeKeeper.HasHandler("v6"))
		plan := upgradetypes.Plan{
			Name:   "v6",
			Time:   time.Now(),
			Height: 1,
			Info:   "info",
		}

		// Note: v5 didn't have the ICA module registered so no params were set
		// but this test explicitly sets the params to values to verify they get
		// overridden during ApplyUpgrade.
		allMessages := []string{"*"}
		ctx := testApp.NewContext(false)
		testApp.ICAHostKeeper.SetParams(ctx, icahosttypes.Params{
			HostEnabled:   false,
			AllowMessages: allMessages,
		})
		got := testApp.ICAHostKeeper.GetParams(ctx)
		require.False(t, got.HostEnabled)
		require.Equal(t, allMessages, got.AllowMessages)

		err := testApp.UpgradeKeeper.ApplyUpgrade(ctx, plan)
		require.NoError(t, err)

		ctx = testApp.NewContext(false)
		got = testApp.ICAHostKeeper.GetParams(ctx)
		require.True(t, got.HostEnabled)
		require.Equal(t, got.AllowMessages, app.IcaAllowMessages())
	})
	t.Run("apply upgrade should set the min commission rate to 10%", func(t *testing.T) {
		consensusParams := app.DefaultConsensusParams()
		consensusParams.Version.App = 5
		testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)
		require.True(t, testApp.UpgradeKeeper.HasHandler("v6"))

		ctx := testApp.NewContext(false)
		oldMinCommissionRate, err := math.LegacyNewDecFromStr("0.05")
		require.NoError(t, err)
		// Set the min commission rate to 5% because that is what is on Mainnet since genesis.
		err = testApp.StakingKeeper.SetParams(ctx, stakingtypes.Params{
			MinCommissionRate: oldMinCommissionRate,
		})
		require.NoError(t, err)
		params, err := testApp.StakingKeeper.GetParams(ctx)
		require.NoError(t, err)
		require.Equal(t, oldMinCommissionRate, params.MinCommissionRate)

		// Apply the upgrade.
		plan := upgradetypes.Plan{
			Name:   "v6",
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
	t.Run("apply upgrade should set the commission rate for a validator to 10% if it was less than that", func(t *testing.T) {
		consensusParams := app.DefaultConsensusParams()
		consensusParams.Version.App = 5
		testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)
		require.True(t, testApp.UpgradeKeeper.HasHandler("v6"))

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
			Name:   "v6",
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
		name                 string
		initialRate          string
		initialMaxRate       string
		initialMaxChangeRate string
		wantRate             string
		wantMaxRate          string
		wantMaxChangeRate    string
		shouldUpdate         bool
	}{
		{
			name:                 "should increase rate and max rate to min commission rate",
			initialRate:          "0.05", // 5%
			initialMaxRate:       "0.08", // 8%
			initialMaxChangeRate: "0.01", // 1%
			wantRate:             "0.10", // 10% (MinCommissionRate)
			wantMaxRate:          "0.10", // 10% (MinCommissionRate)
			wantMaxChangeRate:    "0.01", // unchanged
			shouldUpdate:         true,
		},
		{
			name:                 "should increase rate to min commission rate and leave max rate unchanged",
			initialRate:          "0.03", // 3%
			initialMaxRate:       "0.15", // 15%
			initialMaxChangeRate: "0.02", // 2%
			wantRate:             "0.10", // 10% (MinCommissionRate)
			wantMaxRate:          "0.15", // unchanged
			wantMaxChangeRate:    "0.02", // unchanged
			shouldUpdate:         true,
		},
		{
			name:                 "should increase max rate to min commission rate and leave initial rate unchanged even though this can't happen in practice",
			initialRate:          "0.12", // 12%
			initialMaxRate:       "0.07", // 7%
			initialMaxChangeRate: "0.03", // 3%
			wantRate:             "0.12", // unchanged
			wantMaxRate:          "0.10", // 10% (MinCommissionRate)
			wantMaxChangeRate:    "0.03", // unchanged
			shouldUpdate:         true,
		},
		{
			name:                 "should not update if both rate and max rate are above min commission rate",
			initialRate:          "0.15", // 15%
			initialMaxRate:       "0.20", // 20%
			initialMaxChangeRate: "0.05", // 5%
			wantRate:             "0.15", // unchanged
			wantMaxRate:          "0.20", // unchanged
			wantMaxChangeRate:    "0.05", // unchanged
			shouldUpdate:         false,
		},
		{
			name:                 "should not update if both rate and max rate are exactly at min commission rate",
			initialRate:          "0.10", // 10% (exactly minimum)
			initialMaxRate:       "0.10", // 10% (exactly minimum)
			initialMaxChangeRate: "0.10", // 10%
			wantRate:             "0.10", // unchanged
			wantMaxRate:          "0.10", // unchanged
			wantMaxChangeRate:    "0.10", // unchanged
			shouldUpdate:         false,
		},
		{
			name:                 "zero commission rate - should be updated to minimum",
			initialRate:          "0.0",  // 0%
			initialMaxRate:       "0.05", // 5%
			initialMaxChangeRate: "0.1",  // 10%
			wantRate:             "0.1",  // 10% (MinCommissionRate)
			wantMaxRate:          "0.1",  // 10% (MinCommissionRate)
			wantMaxChangeRate:    "0.1",  // unchanged
			shouldUpdate:         true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			consensusParams := app.DefaultConsensusParams()
			testApp, _, _ := util.NewTestAppWithGenesisSet(consensusParams)

			ctx := testApp.NewContext(false).WithBlockTime(util.GenesisTime.Add(time.Hour * 25))

			validator := createValidatorWithCommission(t, testApp, ctx, tc.initialRate, tc.initialMaxRate, tc.initialMaxChangeRate)

			valAddr, err := sdk.ValAddressFromBech32(validator.GetOperator())
			require.NoError(t, err)
			validatorBefore, err := testApp.StakingKeeper.GetValidator(ctx, valAddr)
			require.NoError(t, err)

			assertCommissionRates(t, validatorBefore, tc.initialRate, tc.initialMaxRate, tc.initialMaxChangeRate)

			err = testApp.UpdateValidatorCommissionRates(ctx)
			require.NoError(t, err)

			validatorAfter, err := testApp.StakingKeeper.GetValidator(ctx, valAddr)
			require.NoError(t, err)

			assertCommissionRates(t, validatorAfter, tc.wantRate, tc.wantMaxRate, tc.wantMaxChangeRate)

			if tc.shouldUpdate {
				require.Equal(t, ctx.BlockTime(), validatorAfter.Commission.UpdateTime, "UpdateTime should be set to current block time")
			}
		})
	}
}

// createValidatorWithCommission creates a validator with specific commission
// rates for testing
func createValidatorWithCommission(t *testing.T, testApp *app.App, ctx sdk.Context, rate, maxRate, maxChangeRate string) stakingtypes.Validator {
	commissionRate, err := math.LegacyNewDecFromStr(rate)
	require.NoError(t, err)
	commissionMaxRate, err := math.LegacyNewDecFromStr(maxRate)
	require.NoError(t, err)
	commissionMaxChangeRate, err := math.LegacyNewDecFromStr(maxChangeRate)
	require.NoError(t, err)

	validators, err := testApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	require.Greater(t, len(validators), 0, "Should have at least one validator")

	validator := validators[0]
	validator.Commission = stakingtypes.NewCommission(commissionRate, commissionMaxRate, commissionMaxChangeRate)

	err = testApp.StakingKeeper.SetValidator(ctx, validator)
	require.NoError(t, err)

	return validator
}

// assertCommissionRates verifies that a validator has the expected commission rates
func assertCommissionRates(t *testing.T, validator stakingtypes.Validator, expectedRate, expectedMaxRate, expectedMaxChangeRate string) {
	wantRate, err := math.LegacyNewDecFromStr(expectedRate)
	require.NoError(t, err)
	wantMaxRate, err := math.LegacyNewDecFromStr(expectedMaxRate)
	require.NoError(t, err)
	wantMaxChangeRate, err := math.LegacyNewDecFromStr(expectedMaxChangeRate)
	require.NoError(t, err)

	require.Equal(t, wantRate, validator.Commission.Rate)
	require.Equal(t, wantMaxRate, validator.Commission.MaxRate)
	require.Equal(t, wantMaxChangeRate, validator.Commission.MaxChangeRate)
}
