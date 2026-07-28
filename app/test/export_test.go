package app_test

import (
	"bytes"
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
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ibcclienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	commitmenttypes "github.com/cosmos/ibc-go/v8/modules/core/23-commitment/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
	ibctypes "github.com/cosmos/ibc-go/v8/modules/core/types"
	ibctm "github.com/cosmos/ibc-go/v8/modules/light-clients/07-tendermint"
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

// TestExportForZeroHeightWithJailAllowedAddrs covers the --jail-allowed-addrs
// path, which jails every validator outside the allow list.
func TestExportForZeroHeightWithJailAllowedAddrs(t *testing.T) {
	testApp, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())

	// Advance past the initial height so a carried over unbonding height is visible.
	for range 3 {
		execBlock(t, testApp, func(sdk.Context) {})
	}

	// An allow list that matches no real validator, so all of them get jailed.
	allowed := []string{sdk.ValAddress(bytes.Repeat([]byte{1}, 20)).String()}

	var exported servertypes.ExportedApp
	var err error
	require.NotPanics(t, func() {
		exported, err = testApp.ExportAppStateAndValidators(true, allowed, nil)
	})
	require.NoError(t, err)

	var appState map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(exported.AppState, &appState))
	var staking stakingtypes.GenesisState
	testApp.AppCodec().MustUnmarshalJSON(appState[stakingtypes.ModuleName], &staking)

	require.NotEmpty(t, staking.Validators)
	for _, validator := range staking.Validators {
		require.True(t, validator.Jailed, "validators outside the allow list must be jailed")
		require.Zero(t, validator.UnbondingHeight,
			"unbonding height must not carry the exported chain's height into a genesis that restarts at height 1")
	}
}

// TestExportedGenesisStartsANewChain checks that the exported genesis is usable
// as the genesis of a new chain, which is the point of --for-zero-height.
func TestExportedGenesisStartsANewChain(t *testing.T) {
	testApp, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	execBlock(t, testApp, func(sdk.Context) {})

	exported, err := testApp.ExportAppStateAndValidators(true, nil, nil)
	require.NoError(t, err)

	newChain := util.NewTestApp()
	_, err = newChain.InitChain(&abci.RequestInitChain{
		Time:            util.GenesisTime,
		ChainId:         util.ChainID,
		Validators:      []abci.ValidatorUpdate{},
		ConsensusParams: &exported.ConsensusParams,
		AppStateBytes:   exported.AppState,
		InitialHeight:   1,
	})
	require.NoError(t, err)

	// InitChain's writes only reach the committed store once a block finalizes them.
	execBlock(t, newChain, func(sdk.Context) {})
	require.Equal(t, int64(1), newChain.LastBlockHeight())
}

func TestExportDropsAllLocalhostClientState(t *testing.T) {
	testApp, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())

	// Localhost normally has no consensus states. Add one to cover exported
	// states from versions that did persist them and ensure that removing the
	// client cannot leave an invalid, orphaned consensus-state entry.
	execBlock(t, testApp, func(ctx sdk.Context) {
		testApp.IBCKeeper.ClientKeeper.SetClientConsensusState(
			ctx,
			ibcexported.LocalhostClientID,
			ibcclienttypes.NewHeight(0, 1),
			ibctm.NewConsensusState(
				util.GenesisTime,
				commitmenttypes.NewMerkleRoot(bytes.Repeat([]byte{1}, 32)),
				bytes.Repeat([]byte{2}, 32),
			),
		)
	})

	exported, err := testApp.ExportAppStateAndValidators(true, nil, nil)
	require.NoError(t, err)

	var appState map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(exported.AppState, &appState))
	var ibcGenesis ibctypes.GenesisState
	testApp.AppCodec().MustUnmarshalJSON(appState[ibcexported.ModuleName], &ibcGenesis)

	for _, client := range ibcGenesis.ClientGenesis.Clients {
		require.NotEqual(t, ibcexported.LocalhostClientID, client.ClientId)
	}
	for _, metadata := range ibcGenesis.ClientGenesis.ClientsMetadata {
		require.NotEqual(t, ibcexported.LocalhostClientID, metadata.ClientId)
	}
	for _, consensus := range ibcGenesis.ClientGenesis.ClientsConsensus {
		require.NotEqual(t, ibcexported.LocalhostClientID, consensus.ClientId)
	}
	require.NoError(t, ibcGenesis.ClientGenesis.Validate())
}

// TestExportForZeroHeightCreditsRewardsToAccounts asserts where the rewards owed
// at export time end up: credited to the delegator, never reassigned to the
// community pool. prepForZeroHeightGenesis donates whatever is still outstanding
// after the withdrawals to the community pool, so any reward the withdrawals miss
// is silently taken from its owner.
//
// The chain under test is itself launched from a --for-zero-height export, because
// that is what makes every delegation start at height zero: distribution's
// InitGenesis re-imports the starting infos verbatim and staking skips its hooks for
// an exported genesis. Such a delegation used to look like it had "started this
// block" against a zero context height, so nothing was withdrawn for it and its
// rewards fell to the community pool.
func TestExportForZeroHeightCreditsRewardsToAccounts(t *testing.T) {
	const (
		delegator = "delegator"
		reward    = 1_000_001
	)
	firstChain, kr := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), delegator)

	record, err := kr.Key(delegator)
	require.NoError(t, err)
	delegatorAddr, err := record.GetAddress()
	require.NoError(t, err)

	execBlock(t, firstChain, func(ctx sdk.Context) {
		validators, err := firstChain.StakingKeeper.GetAllValidators(ctx)
		require.NoError(t, err)
		_, err = firstChain.StakingKeeper.Delegate(
			ctx, delegatorAddr, math.NewInt(1_000_003), stakingtypes.Unbonded, validators[0], true,
		)
		require.NoError(t, err)
	})

	spoon, err := firstChain.ExportAppStateAndValidators(true, nil, nil)
	require.NoError(t, err)

	// Launch the second chain from that genesis, at initial height 1 like a real
	// network. Its delegations now carry a starting height of zero.
	secondChain := util.NewTestApp()
	_, err = secondChain.InitChain(&abci.RequestInitChain{
		Time:            util.GenesisTime,
		ChainId:         util.ChainID,
		Validators:      []abci.ValidatorUpdate{},
		ConsensusParams: &spoon.ConsensusParams,
		AppStateBytes:   spoon.AppState,
		InitialHeight:   1,
	})
	require.NoError(t, err)

	var valAddr sdk.ValAddress
	execBlock(t, secondChain, func(ctx sdk.Context) {
		validators, err := secondChain.StakingKeeper.GetAllValidators(ctx)
		require.NoError(t, err)
		valAddr, err = sdk.ValAddressFromBech32(validators[0].GetOperator())
		require.NoError(t, err)

		info, err := secondChain.DistrKeeper.GetDelegatorStartingInfo(ctx, valAddr, delegatorAddr)
		require.NoError(t, err)
		require.Zero(t, info.Height, "an imported delegation starts at height zero")
	})

	execBlock(t, secondChain, func(ctx sdk.Context) {
		validator, err := secondChain.StakingKeeper.GetValidator(ctx, valAddr)
		require.NoError(t, err)
		require.NoError(t, secondChain.DistrKeeper.AllocateTokensToValidator(
			ctx, validator, sdk.NewDecCoins(sdk.NewInt64DecCoin(appconsts.BondDenom, reward)),
		))
		// Back the reward accounting with real coins so the withdrawals can settle.
		require.NoError(t, secondChain.BankKeeper.SendCoinsFromAccountToModule(
			ctx, delegatorAddr, distributiontypes.ModuleName,
			sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, reward)),
		))
	})

	ctx := secondChain.NewUncachedContext(false, cmtproto.Header{Height: secondChain.LastBlockHeight()})
	balanceBefore := secondChain.BankKeeper.GetBalance(ctx, delegatorAddr, appconsts.BondDenom).Amount
	delegations, err := secondChain.StakingKeeper.GetAllDelegations(ctx)
	require.NoError(t, err)
	outstanding, err := secondChain.DistrKeeper.GetValidatorOutstandingRewardsCoins(ctx, valAddr)
	require.NoError(t, err)
	require.False(t, outstanding.IsZero(), "the test needs rewards to be owed")
	feePoolBefore, err := secondChain.DistrKeeper.FeePool.Get(ctx)
	require.NoError(t, err)

	exported, err := secondChain.ExportAppStateAndValidators(true, nil, nil)
	require.NoError(t, err)

	var appState map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(exported.AppState, &appState))
	var bankGenesis banktypes.GenesisState
	secondChain.AppCodec().MustUnmarshalJSON(appState[banktypes.ModuleName], &bankGenesis)
	var distribution distributiontypes.GenesisState
	secondChain.AppCodec().MustUnmarshalJSON(appState[distributiontypes.ModuleName], &distribution)

	var credited math.Int
	for _, balance := range bankGenesis.Balances {
		if balance.Address == delegatorAddr.String() {
			credited = balance.Coins.AmountOf(appconsts.BondDenom).Sub(balanceBefore)
		}
	}
	require.False(t, credited.IsNil(), "no exported balance for the delegator")
	require.True(t, credited.IsPositive(),
		"the delegator's rewards must be withdrawn to its account, not donated to the community pool")

	// The community pool may only pick up truncation dust, since balances are
	// integers: at most one sub-unit remainder per withdrawal, plus one for the
	// validator's commission. Anything more means rewards were reassigned to it.
	donated := distribution.FeePool.CommunityPool.Sub(feePoolBefore.CommunityPool)
	dustBound := math.LegacyNewDec(int64(len(delegations) + 1))
	require.True(t,
		donated.AmountOf(appconsts.BondDenom).LT(dustBound),
		"community pool absorbed %s, more than truncation dust over %d withdrawals: rewards were diverted from their account",
		donated, len(delegations),
	)
}

// TestExportForZeroHeightFailsWhenRewardsCannotBeWithdrawn covers the other way
// rewards can end up in the community pool: a withdrawal that fails. Those errors
// used to be discarded, leaving the rewards outstanding for the scraps donation to
// hand to the community pool, so the export has to refuse instead.
func TestExportForZeroHeightFailsWhenRewardsCannotBeWithdrawn(t *testing.T) {
	const delegator = "delegator"
	testApp, kr := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), delegator)

	record, err := kr.Key(delegator)
	require.NoError(t, err)
	delegatorAddr, err := record.GetAddress()
	require.NoError(t, err)

	var valAddr sdk.ValAddress
	execBlock(t, testApp, func(ctx sdk.Context) {
		validators, err := testApp.StakingKeeper.GetAllValidators(ctx)
		require.NoError(t, err)
		valAddr, err = sdk.ValAddressFromBech32(validators[0].GetOperator())
		require.NoError(t, err)
		_, err = testApp.StakingKeeper.Delegate(
			ctx, delegatorAddr, math.NewInt(1_000_000), stakingtypes.Unbonded, validators[0], true,
		)
		require.NoError(t, err)
	})

	// Drop the delegation's starting info, which is the state a withdrawal cannot
	// recover from: distribution has no period to calculate rewards from.
	execBlock(t, testApp, func(ctx sdk.Context) {
		require.NoError(t, testApp.DistrKeeper.DeleteDelegatorStartingInfo(ctx, valAddr, delegatorAddr))
	})

	require.Panics(t, func() {
		_, _ = testApp.ExportAppStateAndValidators(true, nil, nil)
	}, "an export that cannot withdraw a delegator's rewards must fail rather than donate them")
}
