package app_test

import (
	"testing"

	"cosmossdk.io/core/appmodule"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	valaddrtypes "github.com/celestiaorg/celestia-app/v11/x/valaddr/types"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto/ed25519"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/distribution"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

func TestFibreRewardDistribution(t *testing.T) {
	for _, tc := range []struct {
		name       string
		version    uint64
		registered int
		jailed     bool
		unbonding  bool
		emptyVotes bool
	}{
		{name: "v10 unchanged", version: 10},
		{name: "v11 no registrations", version: 11},
		{name: "v11 mixed registrations", version: 11, registered: 1},
		{name: "v11 all registered", version: 11, registered: 2},
		{name: "later versions", version: 12, registered: 1},
		{name: "jailed validator exempt", version: 11, registered: 1, jailed: true},
		{name: "unbonding validator exempt", version: 11, registered: 1, unbonding: true},
		{name: "empty votes", version: 11, emptyVotes: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, ctx, validators, votes := setupFibreDistribution(t)
			// Earn rewards before activation. Withholding must preserve these claims.
			fundFibreDistribution(t, a, ctx)
			require.NoError(t, distribution.BeginBlocker(ctx, a.DistrKeeper))
			for i := 0; i < tc.registered; i++ {
				require.NoError(t, a.ValAddrKeeper.SetFibreProviderInfo(ctx, sdk.ConsAddress(votes[i].Validator.Address), valaddrtypes.FibreProviderInfo{Host: "fibre.example.com:7980"}))
			}
			validators[1].Jailed = tc.jailed
			if tc.unbonding {
				validators[1].Status = stakingtypes.Unbonding
			}
			require.NoError(t, a.StakingKeeper.SetValidator(ctx, validators[1]))
			if tc.emptyVotes {
				votes = nil
			}
			cp := ctx.ConsensusParams()
			cp.Version.App = tc.version
			ctx = ctx.WithConsensusParams(cp).WithVoteInfos(votes)
			fundFibreDistribution(t, a, ctx)
			// Compare with the real SDK allocator on the same pre-distribution state.
			baseline, _ := ctx.CacheContext()
			require.NoError(t, distribution.BeginBlocker(baseline, a.DistrKeeper))
			poolBefore, err := a.DistrKeeper.FeePool.Get(baseline)
			require.NoError(t, err)
			expectedPool := poolBefore.CommunityPool
			before := make([]sdk.DecCoins, len(validators))
			commissionsBefore := make([]distrtypes.ValidatorAccumulatedCommission, len(validators))
			currentBefore := make([]distrtypes.ValidatorCurrentRewards, len(validators))
			for i, val := range validators {
				addr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
				require.NoError(t, err)
				before[i], err = a.DistrKeeper.GetValidatorOutstandingRewardsCoins(ctx, addr)
				require.NoError(t, err)
				commissionsBefore[i], err = a.DistrKeeper.GetValidatorAccumulatedCommission(ctx, addr)
				require.NoError(t, err)
				currentBefore[i], err = a.DistrKeeper.GetValidatorCurrentRewards(ctx, addr)
				require.NoError(t, err)
				if tc.version >= 11 && !tc.emptyVotes && i >= tc.registered && val.IsBonded() && !val.IsJailed() {
					normal, err := a.DistrKeeper.GetValidatorOutstandingRewardsCoins(baseline, addr)
					require.NoError(t, err)
					expectedPool = expectedPool.Add(normal.Sub(before[i])...)
				}
			}
			blocker := a.ModuleManager.Modules[distrtypes.ModuleName].(appmodule.HasBeginBlocker)
			require.NoError(t, blocker.BeginBlock(ctx))
			for i, val := range validators {
				addr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
				require.NoError(t, err)
				withheld := tc.version >= 11 && !tc.emptyVotes && i >= tc.registered && val.IsBonded() && !val.IsJailed()
				got, err := a.DistrKeeper.GetValidatorOutstandingRewardsCoins(ctx, addr)
				require.NoError(t, err)
				want, err := a.DistrKeeper.GetValidatorOutstandingRewardsCoins(baseline, addr)
				require.NoError(t, err)
				if withheld {
					want = before[i]
					commission, err := a.DistrKeeper.GetValidatorAccumulatedCommission(ctx, addr)
					require.NoError(t, err)
					require.Equal(t, commissionsBefore[i], commission)
					current, err := a.DistrKeeper.GetValidatorCurrentRewards(ctx, addr)
					require.NoError(t, err)
					require.Equal(t, currentBefore[i], current)
				}
				require.Equal(t, want, got)
				if !withheld {
					normalCommission, err := a.DistrKeeper.GetValidatorAccumulatedCommission(baseline, addr)
					require.NoError(t, err)
					commission, err := a.DistrKeeper.GetValidatorAccumulatedCommission(ctx, addr)
					require.NoError(t, err)
					require.Equal(t, normalCommission, commission)
				}
			}
			pool, err := a.DistrKeeper.FeePool.Get(ctx)
			require.NoError(t, err)
			require.Equal(t, expectedPool, pool.CommunityPool)
			proposer, err := a.DistrKeeper.GetPreviousProposerConsAddr(ctx)
			require.NoError(t, err)
			require.Equal(t, sdk.ConsAddress(ctx.BlockHeader().ProposerAddress), proposer)
			require.Equal(t, votes, ctx.VoteInfos())
			for _, invariant := range []sdk.Invariant{distrkeeper.ModuleAccountInvariant(a.DistrKeeper), distrkeeper.CanWithdrawInvariant(a.DistrKeeper)} {
				message, broken := invariant(ctx)
				require.False(t, broken, message)
			}
		})
	}
}

func TestFibreRegistrationRestoresFutureRewards(t *testing.T) {
	a, ctx, validators, votes := setupFibreDistribution(t)
	cp := ctx.ConsensusParams()
	cp.Version.App = 11
	ctx = ctx.WithConsensusParams(cp)
	blocker := a.ModuleManager.Modules[distrtypes.ModuleName].(appmodule.HasBeginBlocker)
	fundFibreDistribution(t, a, ctx)
	require.NoError(t, blocker.BeginBlock(ctx))
	addr, err := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
	require.NoError(t, err)
	rewards, err := a.DistrKeeper.GetValidatorOutstandingRewardsCoins(ctx, addr)
	require.NoError(t, err)
	require.True(t, rewards.IsZero())
	poolBefore, err := a.DistrKeeper.FeePool.Get(ctx)
	require.NoError(t, err)
	require.NoError(t, a.ValAddrKeeper.SetFibreProviderInfo(ctx, sdk.ConsAddress(votes[0].Validator.Address), valaddrtypes.FibreProviderInfo{Host: "fibre.example.com:7980"}))
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	fundFibreDistribution(t, a, ctx)
	baseline, _ := ctx.CacheContext()
	require.NoError(t, distribution.BeginBlocker(baseline, a.DistrKeeper))
	expectedRewards, err := a.DistrKeeper.GetValidatorOutstandingRewardsCoins(baseline, addr)
	require.NoError(t, err)
	require.NoError(t, blocker.BeginBlock(ctx))
	rewards, err = a.DistrKeeper.GetValidatorOutstandingRewardsCoins(ctx, addr)
	require.NoError(t, err)
	require.False(t, rewards.IsZero())
	poolAfter, err := a.DistrKeeper.FeePool.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, expectedRewards, rewards)
	// Previously withheld rewards stay in the community pool. Only the newly
	// registered validator receives a share of this block's collection.
	expectedPool := poolBefore.CommunityPool.Add(sdk.NewDecCoinsFromCoins(fibreDistributionFees()...)...).Sub(expectedRewards)
	require.Equal(t, expectedPool, poolAfter.CommunityPool)
}

func setupFibreDistribution(t *testing.T) (*app.App, sdk.Context, []stakingtypes.Validator, []abci.VoteInfo) {
	t.Helper()
	a := util.NewTestApp()
	genesis, _, _ := util.GenesisStateWithSingleValidator(a)
	var staking stakingtypes.GenesisState
	a.AppCodec().MustUnmarshalJSON(genesis[stakingtypes.ModuleName], &staking)
	pk, err := codec.FromCmtPubKeyInterface(ed25519.GenPrivKeyFromSecret([]byte("second fibre reward validator")).PubKey())
	require.NoError(t, err)
	pkAny, err := codectypes.NewAnyWithValue(pk)
	require.NoError(t, err)
	second := staking.Validators[0]
	second.OperatorAddress = sdk.ValAddress(pk.Address()).String()
	second.ConsensusPubkey = pkAny
	second.Tokens = second.Tokens.MulRaw(3)
	staking.Validators = append(staking.Validators, second)
	staking.Delegations = append(staking.Delegations, stakingtypes.NewDelegation(staking.Delegations[0].DelegatorAddress, second.OperatorAddress, second.DelegatorShares))
	genesis[stakingtypes.ModuleName] = a.AppCodec().MustMarshalJSON(&staking)
	var bank banktypes.GenesisState
	a.AppCodec().MustUnmarshalJSON(genesis[banktypes.ModuleName], &bank)
	additional := sdk.NewCoin(appconsts.BondDenom, second.Tokens)
	for i := range bank.Balances {
		if bank.Balances[i].Address == authtypes.NewModuleAddress(stakingtypes.BondedPoolName).String() {
			bank.Balances[i].Coins = bank.Balances[i].Coins.Add(additional)
		}
	}
	bank.Supply = bank.Supply.Add(additional)
	genesis[banktypes.ModuleName] = a.AppCodec().MustMarshalJSON(&bank)
	a = util.InitialiseTestAppWithGenesis(a, app.DefaultConsensusParams(), genesis)
	ctx := a.NewContext(false).WithBlockHeight(2).WithBlockTime(util.GenesisTime)
	cp := app.DefaultConsensusParams()
	votes := make([]abci.VoteInfo, len(staking.Validators))
	for i, val := range staking.Validators {
		consAddr, err := val.GetConsAddr()
		require.NoError(t, err)
		votes[i] = abci.VoteInfo{Validator: abci.Validator{Address: consAddr, Power: val.Tokens.Quo(sdk.DefaultPowerReduction).Int64()}}
	}
	return a, ctx.WithConsensusParams(*cp).WithVoteInfos(votes), staking.Validators, votes
}

func fundFibreDistribution(t *testing.T, a *app.App, ctx sdk.Context) {
	t.Helper()
	coins := fibreDistributionFees()
	require.NoError(t, a.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coins))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, authtypes.FeeCollectorName, coins))
}

func fibreDistributionFees() sdk.Coins {
	return sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1_000_000), sdk.NewInt64Coin("otherfee", 1_000))
}
