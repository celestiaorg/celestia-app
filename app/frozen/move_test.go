package frozen_test

import (
	"math"
	mrand "math/rand"
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/app/frozen"
	minttypes "github.com/celestiaorg/celestia-app/v11/x/mint/types"
	dbm "github.com/cosmos/cosmos-db"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// snapshot is the state MoveAll changes, read on one context.
type snapshot struct {
	dest, distr, pools sdk.Coins
	valTokens          sdkmath.Int
	destStake          sdkmath.LegacyDec
}

func (c *chain) snapshot(ctx sdk.Context, dest sdk.AccAddress) snapshot {
	k := c.app
	s := snapshot{
		dest:      k.BankKeeper.GetAllBalances(ctx, dest),
		distr:     k.BankKeeper.GetAllBalances(ctx, k.AccountKeeper.GetModuleAddress(distrtypes.ModuleName)),
		pools:     k.BankKeeper.GetAllBalances(ctx, k.AccountKeeper.GetModuleAddress(stakingtypes.BondedPoolName)).Add(k.BankKeeper.GetAllBalances(ctx, k.AccountKeeper.GetModuleAddress(stakingtypes.NotBondedPoolName))...),
		valTokens: sdkmath.ZeroInt(),
		destStake: sdkmath.LegacyZeroDec(),
	}
	vals, err := k.StakingKeeper.GetAllValidators(ctx)
	require.NoError(c.t, err)
	for _, v := range vals {
		s.valTokens = s.valTokens.Add(v.Tokens)
	}
	dels, err := k.StakingKeeper.GetDelegatorDelegations(ctx, dest, math.MaxUint16)
	require.NoError(c.t, err)
	for _, d := range dels {
		v, err := k.StakingKeeper.GetValidator(ctx, c.valAddr(d.ValidatorAddress))
		require.NoError(c.t, err)
		s.destStake = s.destStake.Add(v.TokensFromShares(d.Shares))
	}
	return s
}

// holdings is what a listed account held right before the move.
type holdings struct {
	liquid            sdk.Coins
	staked, unbonding sdkmath.Int
	delegations       int
	onJailed          sdkmath.Int // staked or unbonding at the jailed validator
	unbondingIDs      []uint64
	parked, redels    int
	lastCompletion    time.Time
}

func (c *chain) holdings(ctx sdk.Context, a sdk.AccAddress, jailed sdk.ValAddress) holdings {
	k := c.app.StakingKeeper
	h := holdings{liquid: c.app.BankKeeper.GetAllBalances(ctx, a), staked: sdkmath.ZeroInt(), unbonding: sdkmath.ZeroInt(), onJailed: sdkmath.ZeroInt()}
	dels, err := k.GetDelegatorDelegations(ctx, a, math.MaxUint16)
	require.NoError(c.t, err)
	for _, d := range dels {
		v, err := k.GetValidator(ctx, c.valAddr(d.ValidatorAddress))
		require.NoError(c.t, err)
		amt := v.TokensFromShares(d.Shares).TruncateInt()
		h.staked = h.staked.Add(amt)
		h.delegations++
		if d.ValidatorAddress == jailed.String() {
			h.onJailed = h.onJailed.Add(amt)
		}
	}
	reds, err := k.GetRedelegations(ctx, a, math.MaxUint16)
	require.NoError(c.t, err)
	h.redels = len(reds)
	require.NoError(c.t, c.app.DistrKeeper.UserOutstandingRewards.Walk(ctx, collections.NewPrefixedPairRange[sdk.AccAddress, sdk.ValAddress](a),
		func(collections.Pair[sdk.AccAddress, sdk.ValAddress], distrtypes.UserOutstandingRewards) (bool, error) {
			h.parked++
			return false, nil
		}))
	ubds, err := k.GetUnbondingDelegations(ctx, a, math.MaxUint16)
	require.NoError(c.t, err)
	for _, u := range ubds {
		for _, e := range u.Entries {
			h.unbonding = h.unbonding.Add(e.Balance)
			h.unbondingIDs = append(h.unbondingIDs, e.UnbondingId)
			if e.CompletionTime.After(h.lastCompletion) {
				h.lastCompletion = e.CompletionTime
			}
			if u.ValidatorAddress == jailed.String() {
				h.onJailed = h.onJailed.Add(e.Balance)
			}
		}
	}
	return h
}

func TestMoveAll(t *testing.T) {
	c := newChain(t, dbm.NewMemDB(), 10)
	app, k := c.app, c.app.StakingKeeper
	r := mrand.New(mrand.NewSource(1))
	dest := sdk.MustAccAddressFromBech32(frozen.Destination)
	jailed := c.vals[0]
	thief, bystander := randAddr(), randAddr()
	const otherDenom = "ibc/0000000000000000000000000000000000000000000000000000000000000001"

	victims := make([]sdk.AccAddress, 300)
	for i := range victims {
		victims[i] = randAddr()
	}
	vesting, customWithdraw, ghostParked := victims[1], victims[2], victims[7]

	c.block(func(ctx sdk.Context) {
		base := app.AccountKeeper.NewAccountWithAddress(ctx, vesting).(*authtypes.BaseAccount)
		orig := sdk.NewCoins(sdk.NewInt64Coin(denom, 400_000_000))
		acc, err := vestingtypes.NewContinuousVestingAccount(base, orig, c.now.Unix(), c.now.Add(365*24*time.Hour).Unix())
		require.NoError(t, err)
		app.AccountKeeper.SetAccount(ctx, acc)
		c.mintCoins(ctx, vesting, orig)
		// Delegates locked coins, so DelegatedVesting is non-zero.
		for _, va := range c.vals[:3] {
			v, err := k.GetValidator(ctx, va)
			require.NoError(t, err)
			_, err = k.Delegate(ctx, vesting, sdkmath.NewInt(100_000_000), stakingtypes.Unbonded, v, true)
			require.NoError(t, err)
		}

		for i, a := range victims {
			if i%4 != 3 {
				for _, j := range r.Perm(len(c.vals))[:1+r.Intn(4)] {
					c.delegate(ctx, a, c.vals[j], 10_000_000+r.Int63n(90_000_000))
				}
			}
			if i%3 == 0 {
				// Two entries at one validator, plus a full undelegation elsewhere.
				va := c.vals[r.Intn(len(c.vals))]
				sh := c.delegate(ctx, a, va, 50_000_000)
				_, _, err := k.Undelegate(ctx, a, va, sh.QuoInt64(4))
				require.NoError(t, err)
				_, _, err = k.Undelegate(ctx, a, va, sh.QuoInt64(4))
				require.NoError(t, err)
			}
			if i%5 == 0 {
				c.mintCoins(ctx, a, sdk.NewCoins(sdk.NewInt64Coin(denom, 5_000_000), sdk.NewInt64Coin(otherDenom, 7)))
			}
		}
		require.NoError(t, app.DistrKeeper.SetDelegatorWithdrawAddr(ctx, customWithdraw, thief))
		c.delegate(ctx, customWithdraw, c.vals[1], 30_000_000)
		c.delegate(ctx, bystander, c.vals[1], 30_000_000)
	})

	c.block(func(ctx sdk.Context) { c.allocateRewards(ctx, 1_000_000_000) })

	c.block(func(ctx sdk.Context) {
		for i, a := range victims {
			dels, err := k.GetDelegatorDelegations(ctx, a, math.MaxUint16)
			require.NoError(t, err)
			if len(dels) == 0 {
				continue
			}
			d := dels[0]
			va := c.valAddr(d.ValidatorAddress)
			switch i % 6 {
			case 1: // Full undelegation parks rewards for a validator a no longer delegates to.
				_, _, err = k.Undelegate(ctx, a, va, d.Shares)
				require.NoError(t, err)
			case 2: // Partial undelegation parks rewards next to a live delegation.
				_, _, err = k.Undelegate(ctx, a, va, d.Shares.QuoInt64(3))
				require.NoError(t, err)
			case 4: // Redelegation records stay behind on the listed account.
				dst := c.vals[(r.Intn(len(c.vals)-1)+1)%len(c.vals)]
				if !dst.Equals(va) {
					_, err = k.BeginRedelegation(ctx, a, va, dst, d.Shares.QuoInt64(2))
					require.NoError(t, err)
				}
			}
		}
		// Parked rewards for a validator that no longer exists.
		ghost := sdk.ValAddress(randAddr())
		rew := sdk.NewCoins(sdk.NewInt64Coin(denom, 123_456))
		require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, rew))
		require.NoError(t, app.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, distrtypes.ModuleName, rew))
		require.NoError(t, app.DistrKeeper.UserOutstandingRewards.Set(ctx, collections.Join(ghostParked, ghost), distrtypes.UserOutstandingRewards{Rewards: rew}))

		// Delegated before the jail so it ends up on a jailed, unbonding validator.
		v, err := k.GetValidator(ctx, jailed)
		require.NoError(t, err)
		cons, err := v.GetConsAddr()
		require.NoError(t, err)
		require.NoError(t, k.Jail(ctx, cons))
	})
	c.block(func(ctx sdk.Context) { c.allocateRewards(ctx, 500_000_000) })
	c.block(func(ctx sdk.Context) {
		_, _, err := k.Undelegate(ctx, bystander, c.vals[1], sdkmath.LegacyNewDec(30_000_000))
		require.NoError(t, err)
	})
	c.block(nil)

	ctx := c.ctx()
	v, err := k.GetValidator(ctx, jailed)
	require.NoError(t, err)
	require.True(t, v.IsJailed())
	require.True(t, v.IsUnbonding())
	c.checkInvariants("before")

	var (
		moved     []frozen.Moved
		before    = map[string]holdings{}
		pre, post snapshot
		events    sdk.Events
		lastDone  time.Time
		thiefBal  = app.BankKeeper.GetAllBalances(ctx, thief)
	)
	c.block(func(ctx sdk.Context) {
		for _, a := range victims {
			h := c.holdings(ctx, a, jailed)
			before[a.String()] = h
			if h.lastCompletion.After(lastDone) {
				lastDone = h.lastCompletion
			}
		}
		pre = c.snapshot(ctx, dest)
		em := sdk.NewEventManager()
		moved, err = frozen.MoveAll(ctx.WithEventManager(em), c.keepers(), victims, dest)
		require.NoError(t, err)
		events = em.Events()
		post = c.snapshot(ctx, dest)
	})
	c.checkInvariants("after")

	ctx = c.ctx()
	require.Len(t, moved, len(victims))
	liquid, rewards := sdk.NewCoins(), sdk.NewCoins()
	staked, unbonding, onJailed := sdkmath.ZeroInt(), sdkmath.ZeroInt(), sdkmath.ZeroInt()
	nEvents, nOps, nParked, nRedels := 0, 0, 0, 0
	for i, a := range victims {
		m, h := moved[i], before[a.String()]
		require.Equal(t, a, m.Address)
		require.Equal(t, h.liquid.String(), m.Liquid.String(), "victim %d liquid", i)
		// Earlier moves at the same validator can shift each truncation by one.
		require.True(t, h.staked.Sub(m.Staked).Abs().LTE(sdkmath.NewInt(int64(h.delegations))), "victim %d staked %s, want %s", i, m.Staked, h.staked)
		require.Equal(t, h.unbonding.String(), m.Unbonding.String(), "victim %d unbonding", i)
		liquid, rewards = liquid.Add(m.Liquid...), rewards.Add(m.Rewards...)
		staked, unbonding, onJailed = staked.Add(m.Staked), unbonding.Add(m.Unbonding), onJailed.Add(h.onJailed)
		nOps += h.delegations + len(h.unbondingIDs)
		nParked, nRedels = nParked+h.parked, nRedels+h.redels

		dels, err := k.GetDelegatorDelegations(ctx, a, math.MaxUint16)
		require.NoError(t, err)
		require.Empty(t, dels)
		ubds, err := k.GetUnbondingDelegations(ctx, a, math.MaxUint16)
		require.NoError(t, err)
		require.Empty(t, ubds)
		require.True(t, app.BankKeeper.GetAllBalances(ctx, a).IsZero(), "victim %d still holds funds", i)
		iter, err := app.DistrKeeper.UserOutstandingRewards.Iterate(ctx, collections.NewPrefixedPairRange[sdk.AccAddress, sdk.ValAddress](a))
		require.NoError(t, err)
		require.False(t, iter.Valid(), "victim %d has parked rewards", i)
		iter.Close()
		w, err := app.DistrKeeper.GetDelegatorWithdrawAddr(ctx, a)
		require.NoError(t, err)
		require.Equal(t, a, w)
		for _, id := range h.unbondingIDs {
			require.False(t, ctx.KVStore(app.GetKey(stakingtypes.StoreKey)).Has(stakingtypes.GetUnbondingIndexKey(id)), "victim %d unbonding id %d", i, id)
		}

		for _, e := range events {
			if e.Type != frozen.EventTypeMove {
				continue
			}
			if attr, _ := e.GetAttribute(frozen.AttributeKeyAddress); attr.Value != a.String() {
				continue
			}
			nEvents++
			for key, want := range map[string]string{
				frozen.AttributeKeyDestination: frozen.Destination,
				frozen.AttributeKeyLiquid:      m.Liquid.String(),
				frozen.AttributeKeyStaked:      sdk.NewCoin(denom, m.Staked).String(),
				frozen.AttributeKeyUnbonding:   sdk.NewCoin(denom, m.Unbonding).String(),
				frozen.AttributeKeyRewards:     m.Rewards.String(),
			} {
				attr, ok := e.GetAttribute(key)
				require.True(t, ok)
				require.Equal(t, want, attr.Value)
			}
		}
	}
	require.Equal(t, len(victims), nEvents)
	require.True(t, onJailed.IsPositive())
	require.Greater(t, nParked, 50)
	require.Greater(t, nRedels, 10)
	require.True(t, rewards.AmountOf(denom).IsPositive())
	require.Equal(t, int64(123_456), moved[7].Rewards.AmountOf(denom).Int64())

	// Stake at the jailed validator is paid out of the pools; everything else is re-delegated.
	// Each Unbond truncates, so pre-move valuations are off by up to one utia per operation.
	tol := sdkmath.NewInt(int64(nOps))
	paidOut := pre.pools.Sub(post.pools...).AmountOf(denom)
	require.True(t, paidOut.Sub(onJailed).Abs().LTE(tol), "paid out %s, want %s", paidOut, onJailed)
	require.Equal(t, liquid.Add(rewards...).Add(sdk.NewCoin(denom, paidOut)).String(), post.dest.Sub(pre.dest...).String())
	require.Equal(t, rewards.String(), pre.distr.Sub(post.distr...).String())
	require.Equal(t, unbonding.Sub(paidOut).String(), post.valTokens.Sub(pre.valTokens).String())
	gotStake := post.destStake.Sub(pre.destStake)
	wantStake := sdkmath.LegacyNewDecFromInt(staked.Add(unbonding).Sub(paidOut))
	require.True(t, gotStake.Sub(wantStake).Abs().LTE(sdkmath.LegacyNewDecFromInt(tol)), "dest stake %s, want %s", gotStake, wantStake)

	require.Equal(t, thiefBal.String(), app.BankKeeper.GetAllBalances(ctx, thief).String())
	acc := app.AccountKeeper.GetAccount(ctx, vesting)
	require.IsType(t, &authtypes.BaseAccount{}, acc)

	// The UBD queue still holds the listed pairs; EndBlocker must skip them.
	destBal := app.BankKeeper.GetAllBalances(ctx, dest)
	bystanderBal := app.BankKeeper.GetBalance(ctx, bystander, denom).Amount
	c.now = lastDone.Add(time.Hour)
	c.block(nil)
	c.block(nil)
	ctx = c.ctx()
	cctx, _ := ctx.CacheContext()
	mature, err := k.DequeueAllMatureUBDQueue(cctx, c.now)
	require.NoError(t, err)
	require.Empty(t, mature)
	require.Equal(t, sdkmath.NewInt(30_000_000).String(), app.BankKeeper.GetBalance(ctx, bystander, denom).Amount.Sub(bystanderBal).String())
	require.Equal(t, destBal.String(), app.BankKeeper.GetAllBalances(ctx, dest).String())
	for i, a := range victims {
		require.True(t, app.BankKeeper.GetAllBalances(ctx, a).IsZero(), "victim %d", i)
	}
	c.checkInvariants("after maturity")
}

func TestMoveAllCreatesDestination(t *testing.T) {
	c := newChain(t, dbm.NewMemDB(), 2)
	k := c.app.StakingKeeper
	victim, dest, jailed := randAddr(), randAddr(), c.vals[0]
	c.block(func(ctx sdk.Context) {
		sh := c.delegate(ctx, victim, jailed, 50_000_000)
		_, _, err := k.Undelegate(ctx, victim, jailed, sh)
		require.NoError(t, err)
		v, err := k.GetValidator(ctx, jailed)
		require.NoError(t, err)
		cons, err := v.GetConsAddr()
		require.NoError(t, err)
		require.NoError(t, k.Jail(ctx, cons))
	})
	c.block(func(ctx sdk.Context) {
		require.False(t, c.app.AccountKeeper.HasAccount(ctx, dest))
		_, err := frozen.MoveAll(ctx, c.keepers(), []sdk.AccAddress{victim}, dest)
		require.NoError(t, err)
		require.Equal(t, int64(50_000_000), c.app.BankKeeper.GetBalance(ctx, dest, denom).Amount.Int64())
	})
	c.checkInvariants("after")
}

func TestMoveAllRejectsDestination(t *testing.T) {
	c := newChain(t, dbm.NewMemDB(), 1)
	dest := sdk.MustAccAddressFromBech32(frozen.Destination)
	_, err := frozen.MoveAll(c.ctx(), c.keepers(), []sdk.AccAddress{randAddr(), dest}, dest)
	require.ErrorContains(t, err, "is listed")
}
