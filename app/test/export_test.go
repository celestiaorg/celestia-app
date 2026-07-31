package app_test

import (
	"encoding/json"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
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
