package app_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// TestV11UpgradePreservesState checks that the v11 upgrade doesn't repeat the
// v10 upgrade's state changes.
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

// TestV11CortoUnbondingTime checks that the v11 upgrade shortens unbonding on
// Corto only.
func TestV11CortoUnbondingTime(t *testing.T) {
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
				Name:   "v11",
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
			completion, _, err := testApp.StakingKeeper.Undelegate(ctx, delAddr, valAddr, math.LegacyNewDec(1))
			require.NoError(t, err)
			require.Equal(t, ctx.BlockTime().Add(want), completion)
		})
	}
}

// TestNewAppWithPendingV11Upgrade checks that a node restarted at the v11
// upgrade height loads its existing stores. The v11 upgrade adds no stores, so
// it must not install an upgrade store loader.
func TestNewAppWithPendingV11Upgrade(t *testing.T) {
	originalHome := app.NodeHome
	app.NodeHome = t.TempDir()
	t.Cleanup(func() { app.NodeHome = originalHome })

	db := tmdb.NewMemDB()
	testApp := app.New(log.NewNopLogger(), db, nil, 0, 0, util.EmptyAppOptions{}, baseapp.SetChainID(util.ChainID))
	genesisState, _, _ := util.GenesisStateWithSingleValidator(testApp)
	testApp = util.InitialiseTestAppWithGenesis(testApp, app.DefaultConsensusParams(), genesisState)
	finalizeAndCommit(t, testApp)

	plan, err := json.Marshal(upgradetypes.Plan{Name: fmt.Sprintf("v%d", appconsts.Version), Height: testApp.LastBlockHeight() + 1})
	require.NoError(t, err)
	upgradeInfoPath := filepath.Join(app.NodeHome, "data", upgradetypes.UpgradeInfoFilename)
	require.NoError(t, os.MkdirAll(filepath.Dir(upgradeInfoPath), 0o700))
	require.NoError(t, os.WriteFile(upgradeInfoPath, plan, 0o600))

	require.NotPanics(t, func() {
		app.New(log.NewNopLogger(), db, nil, 0, 0, util.EmptyAppOptions{}, baseapp.SetChainID(util.ChainID))
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
