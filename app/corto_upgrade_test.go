package app_test

import (
	"testing"
	"time"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/stretchr/testify/require"
)

func TestCortoV12Upgrade(t *testing.T) {
	for _, chain := range []string{appconsts.CortoChainID, appconsts.MochaChainID, appconsts.MainnetChainID, "other"} {
		for _, before := range []time.Duration{10 * time.Minute, 337 * time.Hour} {
			t.Run(chain+before.String(), func(t *testing.T) {
				a, _, _ := util.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
				ctx := a.NewContext(false).WithChainID(chain).WithBlockHeight(1).WithBlockTime(util.GenesisTime)
				params, err := a.StakingKeeper.GetParams(ctx)
				require.NoError(t, err)
				params.UnbondingTime = before
				require.NoError(t, a.StakingKeeper.SetParams(ctx, params))
				cp, err := a.ConsensusKeeper.ParamsStore.Get(ctx)
				require.NoError(t, err)
				cp.Evidence.MaxAgeDuration = 337 * time.Hour
				cp.Evidence.MaxAgeNumBlocks = 404_400
				require.NoError(t, a.ConsensusKeeper.ParamsStore.Set(ctx, cp))
				vals, err := a.StakingKeeper.GetAllValidators(ctx)
				require.NoError(t, err)
				val, err := sdk.ValAddressFromBech32(vals[0].OperatorAddress)
				require.NoError(t, err)
				dels, err := a.StakingKeeper.GetValidatorDelegations(ctx, val)
				require.NoError(t, err)
				del := sdk.MustAccAddressFromBech32(dels[0].DelegatorAddress)
				shares := dels[0].Shares.QuoInt64(4)
				oldCompletion, _, err := a.StakingKeeper.Undelegate(ctx, del, val, shares)
				require.NoError(t, err)
				require.NoError(t, a.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: "v12", Height: 1}))
				want := before
				cp.Evidence.MaxAgeNumBlocks = appconsts.MaxAgeNumBlocks
				if chain == appconsts.CortoChainID {
					want = 10 * time.Minute
					cp.Evidence.MaxAgeDuration, cp.Evidence.MaxAgeNumBlocks = 10*time.Minute, 500
				}
				params.UnbondingTime, params.HistoricalEntries = want, appconsts.HistoricalEntries
				got, err := a.StakingKeeper.GetParams(ctx)
				require.NoError(t, err)
				require.Equal(t, params, got)
				gotCP, err := a.ConsensusKeeper.ParamsStore.Get(ctx)
				require.NoError(t, err)
				require.Equal(t, cp, gotCP)
				old, err := a.StakingKeeper.GetUnbondingDelegation(ctx, del, val)
				require.NoError(t, err)
				require.Equal(t, oldCompletion, old.Entries[0].CompletionTime)
				ctx = ctx.WithBlockHeight(2).WithBlockTime(util.GenesisTime.Add(time.Second))
				completion, amount, err := a.StakingKeeper.Undelegate(ctx, del, val, shares)
				require.NoError(t, err)
				require.Equal(t, ctx.BlockTime().Add(want), completion)
				// Choose the long starting period so only the new entry matures.
				if chain == appconsts.CortoChainID && before > want {
					balance := a.BankKeeper.GetBalance(ctx, del, appconsts.BondDenom)
					_, err = a.StakingKeeper.EndBlocker(ctx.WithBlockTime(completion))
					require.NoError(t, err)
					require.Equal(t, balance.Amount.Add(amount), a.BankKeeper.GetBalance(ctx, del, appconsts.BondDenom).Amount)
				}
			})
		}
	}
}

func TestCortoEvidenceRequiresBothBoundsToExpire(t *testing.T) {
	for _, tc := range []struct {
		name   string
		blocks int64
		age    time.Duration
		slash  bool
	}{
		{"both at boundary", 500, 10 * time.Minute, true},
		{"only time expired", 500, 10*time.Minute + time.Second, true},
		{"only blocks expired", 501, 10 * time.Minute, true},
		{"both expired", 501, 10*time.Minute + time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, _ := util.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
			ctx := a.NewContext(false).WithChainID(appconsts.CortoChainID).WithBlockHeight(1).WithBlockTime(util.GenesisTime)
			require.NoError(t, a.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: "v12", Height: 1}))
			cp, err := a.ConsensusKeeper.ParamsStore.Get(ctx)
			require.NoError(t, err)
			vals, err := a.StakingKeeper.GetAllValidators(ctx)
			require.NoError(t, err)
			cons, err := vals[0].GetConsAddr()
			require.NoError(t, err)
			require.NoError(t, a.SlashingKeeper.SetValidatorSigningInfo(ctx, cons, slashingtypes.NewValidatorSigningInfo(cons, 0, 0, time.Time{}, false, 0)))
			evidence := abci.Misbehavior{Type: abci.MisbehaviorType_DUPLICATE_VOTE, Height: 1, Time: util.GenesisTime, Validator: abci.Validator{Address: cons, Power: vals[0].ConsensusPower(a.StakingKeeper.PowerReduction(ctx))}}
			ctx = ctx.WithBlockHeight(1 + tc.blocks).WithBlockTime(util.GenesisTime.Add(tc.age)).WithConsensusParams(cp).WithCometInfo(baseapp.NewBlockInfo([]abci.Misbehavior{evidence}, nil, nil, abci.CommitInfo{}))
			require.NoError(t, a.EvidenceKeeper.BeginBlocker(ctx))
			require.Equal(t, tc.slash, a.SlashingKeeper.IsTombstoned(ctx, cons))
		})
	}
}
