package frozen

import (
	"errors"
	"fmt"
	"math"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestexported "github.com/cosmos/cosmos-sdk/x/auth/vesting/exported"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// EventTypeMove is emitted once per listed address with the amounts moved.
const EventTypeMove = "frozen_move"

const (
	AttributeKeyAddress     = "address"
	AttributeKeyDestination = "destination"
	AttributeKeyLiquid      = "liquid"
	AttributeKeyStaked      = "staked"
	AttributeKeyUnbonding   = "unbonding"
	AttributeKeyRewards     = "rewards"
)

// Keepers are the keepers MoveAll reads and writes.
type Keepers struct {
	Account authkeeper.AccountKeeper
	Bank    bankkeeper.Keeper
	Staking *stakingkeeper.Keeper
	Distr   distrkeeper.Keeper
}

// Moved is what MoveAll took from one address, recorded for reimbursement.
type Moved struct {
	Address   sdk.AccAddress
	Liquid    sdk.Coins
	Staked    sdkmath.Int // bond denom, delegations
	Unbonding sdkmath.Int // bond denom, unbonding entries
	Rewards   sdk.Coins
}

// Total returns everything moved from the address.
func (m Moved) Total(bondDenom string) sdk.Coins {
	return m.Liquid.Add(m.Rewards...).Add(sdk.NewCoin(bondDenom, m.Staked.Add(m.Unbonding)))
}

// MoveAll moves every delegation, unbonding entry, reward and balance of addrs to
// dest. Stake stays with the same validators, so voting power doesn't change.
func MoveAll(ctx sdk.Context, k Keepers, addrs []sdk.AccAddress, dest sdk.AccAddress) ([]Moved, error) {
	bondDenom, err := k.Staking.BondDenom(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Moved, 0, len(addrs))
	for _, a := range addrs {
		if a.Equals(dest) {
			return nil, fmt.Errorf("destination %s is listed", dest)
		}
		m, err := move(ctx, k, a, dest, bondDenom)
		if err != nil {
			return nil, fmt.Errorf("moving %s: %w", a, err)
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent(EventTypeMove,
			sdk.NewAttribute(AttributeKeyAddress, a.String()),
			sdk.NewAttribute(AttributeKeyDestination, dest.String()),
			sdk.NewAttribute(AttributeKeyLiquid, m.Liquid.String()),
			sdk.NewAttribute(AttributeKeyStaked, sdk.NewCoin(bondDenom, m.Staked).String()),
			sdk.NewAttribute(AttributeKeyUnbonding, sdk.NewCoin(bondDenom, m.Unbonding).String()),
			sdk.NewAttribute(AttributeKeyRewards, m.Rewards.String()),
		))
		out = append(out, m)
	}
	return out, nil
}

func move(ctx sdk.Context, k Keepers, a, dest sdk.AccAddress, bondDenom string) (Moved, error) {
	m := Moved{Address: a, Liquid: sdk.NewCoins(), Staked: sdkmath.ZeroInt(), Unbonding: sdkmath.ZeroInt(), Rewards: sdk.NewCoins()}

	// Stake leaves without passing through the account, so a vesting schedule can't
	// hold; dropping it also sends rewards to the withdraw address and frees all coins.
	if acc := k.Account.GetAccount(ctx, a); acc != nil {
		if _, ok := acc.(vestexported.VestingAccount); ok {
			k.Account.SetAccount(ctx, authtypes.NewBaseAccount(a, acc.GetPubKey(), acc.GetAccountNumber(), acc.GetSequence()))
		}
	}
	dels, err := k.Staking.GetDelegatorDelegations(ctx, a, math.MaxUint16)
	if err != nil {
		return m, err
	}
	// Withdrawals below pay the withdraw address; reset to a itself once done.
	if len(dels) > 0 {
		if err := k.Distr.SetDelegatorWithdrawAddr(ctx, a, dest); err != nil {
			return m, err
		}
	}
	for _, d := range dels {
		valAddr, err := sdk.ValAddressFromBech32(d.ValidatorAddress)
		if err != nil {
			return m, err
		}
		// CIP-30 parks rewards on share changes instead of paying them, so withdraw first.
		r, err := k.Distr.WithdrawDelegationRewards(ctx, a, valAddr)
		if err != nil {
			return m, err
		}
		m.Rewards = m.Rewards.Add(r...)
		amt, err := k.Staking.Unbond(ctx, a, valAddr, d.Shares)
		if err != nil {
			return m, err
		}
		if err := delegate(ctx, k, dest, valAddr, amt, false, bondDenom); err != nil {
			return m, err
		}
		m.Staked = m.Staked.Add(amt)
	}
	if len(dels) > 0 {
		if err := k.Distr.DeleteDelegatorWithdrawAddr(ctx, a, a); err != nil {
			return m, err
		}
	}

	ubds, err := k.Staking.GetUnbondingDelegations(ctx, a, math.MaxUint16)
	if err != nil {
		return m, err
	}
	for _, u := range ubds {
		valAddr, err := sdk.ValAddressFromBech32(u.ValidatorAddress)
		if err != nil {
			return m, err
		}
		total := sdkmath.ZeroInt()
		for _, e := range u.Entries {
			total = total.Add(e.Balance)
			// RemoveUnbondingDelegation leaves the ID index behind; CompleteUnbonding deletes it.
			if err := k.Staking.DeleteUnbondingIndex(ctx, e.UnbondingId); err != nil {
				return m, err
			}
		}
		if err := delegate(ctx, k, dest, valAddr, total, true, bondDenom); err != nil {
			return m, err
		}
		// The queue still holds this pair; EndBlocker skips it once the UBD is gone.
		if err := k.Staking.RemoveUnbondingDelegation(ctx, u); err != nil {
			return m, err
		}
		m.Unbonding = m.Unbonding.Add(total)
	}

	parked, err := sweepParked(ctx, k, a, dest)
	if err != nil {
		return m, err
	}
	m.Rewards = m.Rewards.Add(parked...)

	if bal := k.Bank.GetAllBalances(ctx, a); !bal.IsZero() {
		if err := k.Bank.SendCoins(ctx, a, dest, bal); err != nil {
			return m, err
		}
		m.Liquid = bal
	}
	return m, nil
}

// sweepParked pays dest the rewards CIP-30 parked for a, including entries for
// validators that no longer exist, which WithdrawDelegationRewards rejects.
func sweepParked(ctx sdk.Context, k Keepers, a, dest sdk.AccAddress) (sdk.Coins, error) {
	rng := collections.NewPrefixedPairRange[sdk.AccAddress, sdk.ValAddress](a)
	var keys []collections.Pair[sdk.AccAddress, sdk.ValAddress]
	total := sdk.NewCoins()
	err := k.Distr.UserOutstandingRewards.Walk(ctx, rng,
		func(key collections.Pair[sdk.AccAddress, sdk.ValAddress], v distrtypes.UserOutstandingRewards) (bool, error) {
			keys = append(keys, key)
			total = total.Add(v.Rewards...)
			return false, nil
		})
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		if err := k.Distr.UserOutstandingRewards.Remove(ctx, key); err != nil {
			return nil, err
		}
	}
	if !total.IsZero() {
		if err := k.Bank.SendCoinsFromModuleToAccount(ctx, distrtypes.ModuleName, dest, total); err != nil {
			return nil, err
		}
	}
	return total, nil
}

// delegate stakes amt, already held by the staking pools, to valAddr for dest. If the
// validator is gone, jailed or has an invalid exchange rate, amt is paid out instead.
func delegate(ctx sdk.Context, k Keepers, dest sdk.AccAddress, valAddr sdk.ValAddress, amt sdkmath.Int, fromUnbonding bool, bondDenom string) error {
	if amt.IsZero() {
		return nil
	}
	val, err := k.Staking.GetValidator(ctx, valAddr)
	found := err == nil
	if err != nil && !errors.Is(err, stakingtypes.ErrNoValidatorFound) {
		return err
	}
	if found && !val.IsJailed() && !val.InvalidExRate() {
		src := val.GetStatus()
		if fromUnbonding {
			src = stakingtypes.Unbonding
		}
		_, err = k.Staking.Delegate(ctx, dest, amt, src, val, false)
		return err
	}
	// Unbond leaves tokens in the pool matching the validator's status; unbonding entries are never bonded.
	pool := stakingtypes.NotBondedPoolName
	if found && val.IsBonded() && !fromUnbonding {
		pool = stakingtypes.BondedPoolName
	}
	return k.Bank.UndelegateCoinsFromModuleToAccount(ctx, pool, dest, sdk.NewCoins(sdk.NewCoin(bondDenom, amt)))
}
