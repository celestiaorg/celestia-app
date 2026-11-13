package frozen_test

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/frozen"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	minttypes "github.com/celestiaorg/celestia-app/v11/x/mint/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

const denom = "utia"

// chain drives a test app block by block, committing each block.
type chain struct {
	t    testing.TB
	app  *app.App
	now  time.Time
	vals []sdk.ValAddress
}

func newChain(t testing.TB, db dbm.DB, nVals int) *chain {
	a := app.New(log.NewNopLogger(), db, nil, 0, 0, testutil.EmptyAppOptions{}, baseapp.SetChainID(testutil.ChainID))
	gen, _, _ := testutil.GenesisStateWithSingleValidator(a)
	testutil.InitialiseTestAppWithGenesis(a, app.DefaultConsensusParams(), gen)
	c := &chain{t: t, app: a, now: testutil.GenesisTime}
	c.block(nil) // commits genesis
	c.block(func(ctx sdk.Context) {
		msrv := stakingkeeper.NewMsgServerImpl(a.StakingKeeper)
		for i := range nVals {
			op := randAddr()
			c.mint(ctx, op, 2_000_000_000)
			pk, err := codectypes.NewAnyWithValue(ed25519.GenPrivKey().PubKey())
			require.NoError(t, err)
			_, err = msrv.CreateValidator(ctx, &stakingtypes.MsgCreateValidator{
				Description:       stakingtypes.Description{Moniker: fmt.Sprint(i)},
				Commission:        stakingtypes.NewCommissionRates(sdkmath.LegacyNewDecWithPrec(1, 1), sdkmath.LegacyNewDecWithPrec(2, 1), sdkmath.LegacyNewDecWithPrec(1, 2)),
				MinSelfDelegation: sdkmath.OneInt(),
				ValidatorAddress:  sdk.ValAddress(op).String(),
				Pubkey:            pk,
				Value:             sdk.NewInt64Coin(denom, 1_000_000_000),
			})
			require.NoError(t, err)
			c.vals = append(c.vals, sdk.ValAddress(op))
		}
	})
	return c
}

// block runs work on the working state, then finalizes and commits a block on top of it.
func (c *chain) block(work func(sdk.Context)) (workTime, commitTime time.Duration) {
	c.now = c.now.Add(6 * time.Second)
	h := c.app.LastBlockHeight() + 1
	start := time.Now()
	if work != nil {
		ctx := c.app.NewUncachedContext(false, cmtproto.Header{Height: h, Time: c.now, ChainID: testutil.ChainID})
		cctx, write := ctx.CacheContext()
		work(cctx)
		write()
	}
	workTime = time.Since(start)
	start = time.Now()
	_, err := c.app.FinalizeBlock(&abci.RequestFinalizeBlock{Time: c.now, Height: h, Hash: c.app.LastCommitID().Hash})
	require.NoError(c.t, err)
	_, err = c.app.Commit()
	require.NoError(c.t, err)
	return workTime, time.Since(start)
}

// ctx returns a context on the committed state.
func (c *chain) ctx() sdk.Context {
	return c.app.NewUncachedContext(false, cmtproto.Header{Height: c.app.LastBlockHeight(), Time: c.now, ChainID: testutil.ChainID})
}

func (c *chain) mint(ctx sdk.Context, to sdk.AccAddress, amt int64) {
	c.mintCoins(ctx, to, sdk.NewCoins(sdk.NewInt64Coin(denom, amt)))
}

func (c *chain) mintCoins(ctx sdk.Context, to sdk.AccAddress, coins sdk.Coins) {
	require.NoError(c.t, c.app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coins))
	require.NoError(c.t, c.app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, to, coins))
}

func (c *chain) delegate(ctx sdk.Context, del sdk.AccAddress, val sdk.ValAddress, amt int64) sdkmath.LegacyDec {
	v, err := c.app.StakingKeeper.GetValidator(ctx, val)
	require.NoError(c.t, err)
	c.mint(ctx, del, amt)
	sh, err := c.app.StakingKeeper.Delegate(ctx, del, sdkmath.NewInt(amt), stakingtypes.Unbonded, v, true)
	require.NoError(c.t, err)
	return sh
}

// allocateRewards funds the distribution module and allocates amt to every validator.
func (c *chain) allocateRewards(ctx sdk.Context, amt int64) {
	coins := sdk.NewCoins(sdk.NewInt64Coin(denom, amt*int64(len(c.vals))))
	require.NoError(c.t, c.app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coins))
	require.NoError(c.t, c.app.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, distrtypes.ModuleName, coins))
	for _, va := range c.vals {
		v, err := c.app.StakingKeeper.GetValidator(ctx, va)
		require.NoError(c.t, err)
		require.NoError(c.t, c.app.DistrKeeper.AllocateTokensToValidator(ctx, v, sdk.NewDecCoins(sdk.NewInt64DecCoin(denom, amt))))
	}
}

func (c *chain) keepers() frozen.Keepers {
	return frozen.Keepers{Account: c.app.AccountKeeper, Bank: c.app.BankKeeper, Staking: c.app.StakingKeeper, Distr: c.app.DistrKeeper}
}

func (c *chain) checkInvariants(when string) {
	ctx := c.ctx()
	for name, inv := range map[string]sdk.Invariant{
		"staking": stakingkeeper.AllInvariants(c.app.StakingKeeper),
		"distr":   distrModuleBalance(c.app),
	} {
		msg, broken := inv(ctx)
		require.False(c.t, broken, "%s %s: %s", when, name, msg)
	}
}

// distrModuleBalance is the distribution module-account invariant plus CIP-30
// parked rewards, which the stock invariant doesn't count.
func distrModuleBalance(a *app.App) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		k := a.DistrKeeper
		var expected sdk.DecCoins
		k.IterateValidatorOutstandingRewards(ctx, func(_ sdk.ValAddress, r distrtypes.ValidatorOutstandingRewards) bool {
			expected = expected.Add(r.Rewards...)
			return false
		})
		fp, err := k.FeePool.Get(ctx)
		if err != nil {
			return err.Error(), true
		}
		exp, _ := expected.Add(fp.CommunityPool...).TruncateDecimal()
		parked := sdk.NewCoins()
		if err := k.UserOutstandingRewards.Walk(ctx, nil, func(_ collections.Pair[sdk.AccAddress, sdk.ValAddress], v distrtypes.UserOutstandingRewards) (bool, error) {
			parked = parked.Add(v.Rewards...)
			return false, nil
		}); err != nil {
			return err.Error(), true
		}
		bal := a.BankKeeper.GetAllBalances(ctx, a.AccountKeeper.GetModuleAddress(distrtypes.ModuleName))
		// Truncation per validator can leave the module a few utia above the sum.
		diff, neg := bal.SafeSub(exp.Add(parked...)...)
		broken := neg || !diff.IsAllLTE(sdk.NewCoins(sdk.NewInt64Coin(denom, 1000)))
		return fmt.Sprintf("module %s, outstanding+pool %s, parked %s", bal, exp, parked), broken
	}
}

func (c *chain) valAddr(s string) sdk.ValAddress {
	v, err := sdk.ValAddressFromBech32(s)
	require.NoError(c.t, err)
	return v
}

var addrSeq uint64

// randAddr returns a fresh, deterministic address.
func randAddr() sdk.AccAddress {
	addrSeq++
	h := sha256.Sum256(binary.BigEndian.AppendUint64(nil, addrSeq))
	return sdk.AccAddress(h[:20])
}
