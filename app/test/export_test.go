package app_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	"cosmossdk.io/store/rootmulti"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

func TestExportAppStateAndValidators(t *testing.T) {
	testApp, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), "genesisAcc")
	exported, err := testApp.ExportAppStateAndValidators(true, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, exported)
	require.Equal(t, appconsts.Version, exported.ConsensusParams.Version.App)
}

func TestExportRefusals(t *testing.T) {
	testCases := []struct {
		name            string
		setup           func(*testing.T, *app.App, sdk.Context)
		modulesToExport []string
		wantErr         string
	}{
		{
			name: "upgrade plan for the next height",
			setup: func(t *testing.T, testApp *app.App, ctx sdk.Context) {
				plan := upgradetypes.Plan{Name: "v100", Height: ctx.BlockHeight() + 1}
				require.NoError(t, testApp.UpgradeKeeper.ScheduleUpgrade(ctx, plan))
			},
			wantErr: "export with --height 1 instead",
		},
		{
			name: "upgrade plan for the next height and another app version",
			setup: func(t *testing.T, testApp *app.App, ctx sdk.Context) {
				plan := upgradetypes.Plan{Name: "v100", Height: ctx.BlockHeight() + 1}
				require.NoError(t, testApp.UpgradeKeeper.ScheduleUpgrade(ctx, plan))
				require.NoError(t, testApp.SetAppVersion(ctx, appconsts.Version+1))
			},
			wantErr: "export with --height 1 instead",
		},
		{
			name: "state at another app version",
			setup: func(t *testing.T, testApp *app.App, ctx sdk.Context) {
				require.NoError(t, testApp.SetAppVersion(ctx, appconsts.Version-1))
			},
			wantErr: fmt.Sprintf("use a binary for app version %d (`celestia-appd passthrough v%d export` in the multiplexer build)",
				appconsts.Version-1, appconsts.Version-1),
		},
		{
			name:            "subset of modules",
			modulesToExport: []string{"bank"},
			wantErr:         "exporting a subset of modules is not supported",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testApp, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
			execBlock(t, testApp, func(ctx sdk.Context) {
				if tc.setup != nil {
					tc.setup(t, testApp, ctx)
				}
			})
			require.Equal(t, int64(2), testApp.LastBlockHeight())

			_, err := testApp.ExportAppStateAndValidators(false, nil, tc.modulesToExport)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

// TestExportUsesLastCommitTime checks that a zero-height export starts the
// unbonding of a validator that leaves the set at the last commit time.
func TestExportUsesLastCommitTime(t *testing.T) {
	valSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{
		cmttypes.NewValidator(ed25519.GenPrivKey().PubKey(), 1),
		cmttypes.NewValidator(ed25519.GenPrivKey().PubKey(), 1),
	})
	testApp := util.NewTestApp()
	genesisState, _ := util.GenesisStateWithValidatorSet(testApp, valSet)
	testApp = util.InitialiseTestAppWithGenesis(testApp, app.DefaultConsensusParams(), genesisState)

	// The export recomputes the validator set and drops one validator.
	execBlock(t, testApp, func(ctx sdk.Context) {
		params, err := testApp.StakingKeeper.GetParams(ctx)
		require.NoError(t, err)
		params.MaxValidators = 1
		require.NoError(t, testApp.StakingKeeper.SetParams(ctx, params))
	})

	commitInfo, err := testApp.CommitMultiStore().(*rootmulti.Store).GetCommitInfo(testApp.LastBlockHeight())
	require.NoError(t, err)
	lastBlockTime := util.GenesisTime.Add(time.Duration(testApp.LastBlockHeight()) * time.Second)
	require.True(t, lastBlockTime.Equal(commitInfo.Timestamp), "want %s, got %s", lastBlockTime, commitInfo.Timestamp)

	exported, err := testApp.ExportAppStateAndValidators(true, nil, nil)
	require.NoError(t, err)

	var appState map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(exported.AppState, &appState))
	var staking stakingtypes.GenesisState
	testApp.AppCodec().MustUnmarshalJSON(appState[stakingtypes.ModuleName], &staking)

	var unbonding []stakingtypes.Validator
	for _, validator := range staking.Validators {
		if validator.IsUnbonding() {
			unbonding = append(unbonding, validator)
		}
	}
	require.Len(t, unbonding, 1)
	want := commitInfo.Timestamp.Add(staking.Params.UnbondingTime)
	require.True(t, want.Equal(unbonding[0].UnbondingTime), "want %s, got %s", want, unbonding[0].UnbondingTime)
}

// TestExportForZeroHeightAfterSlashing is a regression test for a panic in
// export --for-zero-height on any chain that slashed a validator after one of
// its delegations was created.
func TestExportForZeroHeightAfterSlashing(t *testing.T) {
	const delegator = "delegator"
	testApp, kr := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), delegator)

	record, err := kr.Key(delegator)
	require.NoError(t, err)
	delegatorAddr, err := record.GetAddress()
	require.NoError(t, err)

	var (
		valAddr  sdk.ValAddress
		consAddr sdk.ConsAddress
	)

	// Delegate above height 0 so that starting_info.height is non-zero.
	execBlock(t, testApp, func(ctx sdk.Context) {
		validators, err := testApp.StakingKeeper.GetAllValidators(ctx)
		require.NoError(t, err)
		require.Len(t, validators, 1)

		valAddr, err = sdk.ValAddressFromBech32(validators[0].GetOperator())
		require.NoError(t, err)
		consAddr, err = validators[0].GetConsAddr()
		require.NoError(t, err)

		_, err = testApp.StakingKeeper.Delegate(
			ctx, delegatorAddr, math.NewInt(1_000_000), stakingtypes.Unbonded, validators[0], true,
		)
		require.NoError(t, err)
	})

	// Slash the validator, which records a slash event above the delegation's
	// starting height. 1% is far larger than the tolerance in the sanity check.
	execBlock(t, testApp, func(ctx sdk.Context) {
		_, err := testApp.StakingKeeper.Slash(
			ctx, consAddr, ctx.BlockHeight()-1, 1, math.LegacyNewDecWithPrec(1, 2),
		)
		require.NoError(t, err)
	})

	// Assert the precondition that used to panic: the delegation's recorded
	// stake now exceeds what its shares are actually worth.
	execBlock(t, testApp, func(ctx sdk.Context) {
		startingInfo, err := testApp.DistrKeeper.GetDelegatorStartingInfo(ctx, valAddr, delegatorAddr)
		require.NoError(t, err)
		require.NotZero(t, startingInfo.Height, "delegation must start above height 0")

		validator, err := testApp.StakingKeeper.GetValidator(ctx, valAddr)
		require.NoError(t, err)
		delegation, err := testApp.StakingKeeper.GetDelegation(ctx, delegatorAddr, valAddr)
		require.NoError(t, err)

		currentStake := validator.TokensFromShares(delegation.GetShares())
		require.True(t, startingInfo.Stake.GT(currentStake),
			"expected stale starting stake %s > current stake %s", startingInfo.Stake, currentStake)
	})

	var exported servertypes.ExportedApp
	require.NotPanics(t, func() {
		exported, err = testApp.ExportAppStateAndValidators(true, nil, nil)
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), exported.Height)

	var appState map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(exported.AppState, &appState))

	var distribution distributiontypes.GenesisState
	testApp.AppCodec().MustUnmarshalJSON(appState[distributiontypes.ModuleName], &distribution)

	// Slash events are deleted and nothing re-creates them, so their absence is
	// a reliable marker that an export really was --for-zero-height. Historical
	// rewards are *not*: prepForZeroHeightGenesis deletes them and then
	// IncrementValidatorPeriod writes new ones while re-initializing.
	require.Empty(t, distribution.ValidatorSlashEvents)

	var staking stakingtypes.GenesisState
	testApp.AppCodec().MustUnmarshalJSON(appState[stakingtypes.ModuleName], &staking)
	validators := make(map[string]stakingtypes.Validator, len(staking.Validators))
	for _, validator := range staking.Validators {
		validators[validator.OperatorAddress] = validator
	}
	delegations := make(map[string]stakingtypes.Delegation, len(staking.Delegations))
	for _, delegation := range staking.Delegations {
		delegations[delegation.ValidatorAddress+"/"+delegation.DelegatorAddress] = delegation
	}

	// This is the invariant hardspoon depends on: after a --for-zero-height
	// export every delegation's starting stake equals what its shares are
	// currently worth, so crediting truncate(starting_info.stake) back to the
	// delegator returns exactly the principal. Before the fix the slashed
	// delegation's starting stake was stale and this did not hold.
	require.NotEmpty(t, distribution.DelegatorStartingInfos)
	for _, info := range distribution.DelegatorStartingInfos {
		require.Zero(t, info.StartingInfo.Height, "starting infos are re-initialized at height 0")

		validator, ok := validators[info.ValidatorAddress]
		require.True(t, ok, "no exported validator for %s", info.ValidatorAddress)
		delegation, ok := delegations[info.ValidatorAddress+"/"+info.DelegatorAddress]
		require.True(t, ok, "no exported delegation for %s", info.DelegatorAddress)

		require.Equal(t,
			validator.TokensFromSharesTruncated(delegation.Shares).String(),
			info.StartingInfo.Stake.String(),
			"starting stake must equal the delegation's current token value",
		)
	}
}

// execBlock finalizes one block, runs fn at that block's height, and commits.
//
// fn has to write through app.cms (NewUncachedContext) rather than the
// finalize-block cache: baseapp flushes that cache inside FinalizeBlock itself,
// so anything written to it after FinalizeBlock returns is silently dropped by
// Commit.
func execBlock(t *testing.T, testApp *app.App, fn func(sdk.Context)) {
	t.Helper()

	height := testApp.LastBlockHeight() + 1
	_, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: height,
		Time:   util.GenesisTime.Add(time.Duration(height) * time.Second),
		Hash:   testApp.LastCommitID().Hash,
	})
	require.NoError(t, err)

	fn(testApp.NewUncachedContext(false, cmtproto.Header{Height: height}))

	_, err = testApp.Commit()
	require.NoError(t, err)
}
