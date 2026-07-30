package hardspoon

import (
	"fmt"
	"sort"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
)

// creditStake returns every delegation's principal and every in-flight
// unbonding to the account that owns it, as liquid tokens.
//
// After a --for-zero-height export, prepForZeroHeightGenesis has withdrawn all
// rewards and commissions into balances and re-initialized every delegation, so
// starting_info.stake is exactly what the delegation's shares are worth. That
// makes the starting infos a complete ledger of delegation principal and means
// no reward math has to be repeated here.
//
// Redelegations need nothing: their tokens are already counted in the
// destination validator's delegation, and so in its starting info.
func (s *spoon) creditStake() error {
	if len(s.distribution.DelegatorStartingInfos) == 0 && len(s.staking.Delegations) > 0 {
		return fmt.Errorf(
			"export has %d delegations but no delegator starting infos to credit them from",
			len(s.staking.Delegations),
		)
	}

	principal := math.ZeroInt()
	for _, info := range s.distribution.DelegatorStartingInfos {
		amount := info.StartingInfo.Stake.TruncateInt()
		s.credit(info.DelegatorAddress, amount)
		principal = principal.Add(amount)
	}

	unbonding := math.ZeroInt()
	entries := 0
	for _, ubd := range s.staking.UnbondingDelegations {
		for _, entry := range ubd.Entries {
			s.credit(ubd.DelegatorAddress, entry.Balance)
			unbonding = unbonding.Add(entry.Balance)
			entries++
		}
	}

	s.report.Credited = Credited{
		Delegations:      len(s.distribution.DelegatorStartingInfos),
		Principal:        principal,
		UnbondingEntries: entries,
		Unbonding:        unbonding,
		Redelegations:    len(s.staking.Redelegations),
	}
	return nil
}

// reconcileStakingPools checks the credited total against what the staking module
// says is staked, and accounts for anything the pool module accounts hold beyond
// that.
//
// These are two independent facts, and comparing the credited total directly
// against the pool balances conflates them.
//
// Whether the crediting is right: every staked token is either some validator's
// tokens or an unbonding delegation entry, and truncating each delegation's
// stake loses under one unit, so the credited total has to land just below that
// derived total. Anything outside that window means the crediting is wrong, and
// since this runs before the pools are dropped it is the last chance to notice.
//
// Whether a pool holds more than its validators account for: it can, for reasons
// that have nothing to do with crediting. mocha-4's bonded pool carries a round
// 1 TIA that no validator's tokens back, left over from a direct send to the pool
// address. Such a surplus is written off with the rest of the module balance,
// which vaporize already reports, so it is surfaced here rather than being fatal.
// A deficit is fatal: it would mean handing out tokens the pool never held.
func (s *spoon) reconcileStakingPools() error {
	// Unbonding delegation entries are held by the not-bonded pool alongside the
	// tokens of every validator that is not bonded.
	derived := map[string]math.Int{
		stakingtypes.BondedPoolName:    math.ZeroInt(),
		stakingtypes.NotBondedPoolName: s.report.Credited.Unbonding,
	}
	for _, validator := range s.staking.Validators {
		pool := stakingtypes.NotBondedPoolName
		if validator.IsBonded() {
			pool = stakingtypes.BondedPoolName
		}
		derived[pool] = derived[pool].Add(validator.Tokens)
	}

	staked := derived[stakingtypes.BondedPoolName].Add(derived[stakingtypes.NotBondedPoolName])
	credited := s.report.Credited.Principal.Add(s.report.Credited.Unbonding)
	dust := staked.Sub(credited)

	denom := s.staking.Params.BondDenom
	held := math.ZeroInt()
	for _, pool := range []string{stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName} {
		balance := s.moduleAccountBalance(pool).AmountOf(denom)
		held = held.Add(balance)
		if surplus := balance.Sub(derived[pool]); !surplus.IsZero() {
			s.report.Credited.Surpluses = append(s.report.Credited.Surpluses, Surplus{
				Pool: pool, Derived: derived[pool], Held: balance, Surplus: surplus,
			})
		}
	}

	s.report.Credited.Staked = staked
	s.report.Credited.Pools = held
	s.report.Credited.Dust = dust

	maxDust := math.NewInt(int64(s.report.Credited.Delegations))
	if dust.IsNegative() || dust.GT(maxDust) {
		return fmt.Errorf(
			"credited stake does not reconcile with the staking module: credited %s, staked %s, "+
				"difference %s is outside [0, %s] (one unit of truncation per delegation). "+
				"A negative difference means over-crediting, which is what a plain `export` "+
				"produces: starting_info.stake is stale for any delegation whose validator was "+
				"later slashed",
			credited, staked, dust, maxDust,
		)
	}
	for _, surplus := range s.report.Credited.Surpluses {
		if surplus.Surplus.IsNegative() {
			return fmt.Errorf(
				"the %s module account holds %s, less than the %s its validators and unbonding "+
					"delegations account for; crediting from the staking state would hand out "+
					"tokens the pool never held",
				surplus.Pool, surplus.Held, surplus.Derived,
			)
		}
	}
	return nil
}

// vaporize drops accounts whose funds cannot survive the regenesis, from both
// the account list and the balance ledger.
//
// Three categories, all deliberate:
//
//   - Module accounts. The staking pools were just credited back to their
//     delegators, so dropping them is net zero. The rest (community pool, fee
//     collector residue, warp collateral, gov deposits) are written off. Module
//     accounts are recreated on demand by their own modules at InitChain, and
//     leaving a pool balance behind would make staking's InitGenesis panic,
//     since it asserts each pool's balance equals its derived tokens.
//   - Interchain accounts. Nobody holds a key for them and the channels that
//     controlled them are gone, so their funds are unreachable either way.
//   - IBC transfer escrow addresses. Their counterparty channels are gone, so
//     the escrowed tokens can never be released.
func (s *spoon) vaporize() error {
	escrow, err := s.escrowAddresses()
	if err != nil {
		return err
	}

	drop := make(map[string]string, len(escrow)+32)
	for _, account := range s.accounts {
		address := account.GetAddress().String()
		switch typed := account.(type) {
		case *authtypes.ModuleAccount:
			drop[address] = "module:" + typed.Name
		case *icatypes.InterchainAccount:
			drop[address] = "interchain-account"
		}
	}
	for address := range escrow {
		// An escrow address is an ordinary account as far as auth is concerned,
		// so it may not have been classified above.
		if _, ok := drop[address]; !ok {
			drop[address] = "ibc-escrow"
		}
	}

	kept := make([]sdk.AccountI, 0, len(s.accounts))
	for _, account := range s.accounts {
		address := account.GetAddress().String()
		category, dropped := drop[address]
		if !dropped {
			kept = append(kept, account)
			continue
		}
		if coins := s.balances[address]; !coins.IsZero() {
			s.report.Vaporized = append(s.report.Vaporized, Vaporized{
				Address:  address,
				Category: category,
				Coins:    coins,
			})
		}
		delete(s.balances, address)
	}
	s.accounts = kept

	// Any balance left on a dropped address that had no auth account.
	for address, category := range drop {
		if coins, ok := s.balances[address]; ok {
			s.report.Vaporized = append(s.report.Vaporized, Vaporized{
				Address:  address,
				Category: category,
				Coins:    coins,
			})
			delete(s.balances, address)
		}
	}

	sort.Slice(s.report.Vaporized, func(i, j int) bool {
		if s.report.Vaporized[i].Category != s.report.Vaporized[j].Category {
			return s.report.Vaporized[i].Category < s.report.Vaporized[j].Category
		}
		return s.report.Vaporized[i].Address < s.report.Vaporized[j].Address
	})
	return nil
}

// escrowAddresses derives the escrow address of every transfer channel and
// checks the total they hold against the transfer module's own accounting.
//
// This check is what makes dropping escrow balances safe. The addresses are
// derived, not labelled, so a mistake in the derivation would silently delete
// live user balances. transfer.total_escrowed is maintained independently by the
// transfer module, so requiring the two to agree turns any such mistake into a
// failed run.
func (s *spoon) escrowAddresses() (map[string]struct{}, error) {
	addresses := make(map[string]struct{}, len(s.channels))
	held := sdk.NewCoins()
	for _, ch := range s.channels {
		if ch.PortID != ibctransfertypes.PortID {
			continue
		}
		address := ibctransfertypes.GetEscrowAddress(ch.PortID, ch.ChannelID).String()
		addresses[address] = struct{}{}
		held = held.Add(s.balances[address]...)
	}

	if !held.Equal(s.transfer.TotalEscrowed) {
		return nil, fmt.Errorf(
			"derived IBC escrow balances do not match transfer.total_escrowed: derived %s over %d "+
				"transfer channels, module reports %s. Refusing to drop balances that cannot be "+
				"accounted for; either the escrow derivation is wrong or something was sent "+
				"directly to an escrow address",
			held, len(addresses), s.transfer.TotalEscrowed,
		)
	}

	s.report.EscrowChannels = len(addresses)
	s.report.EscrowHeld = held
	return addresses, nil
}

// zeroVestingDelegations clears the delegation tracking on vesting accounts.
//
// delegated_vesting and delegated_free record how much of a vesting schedule is
// currently staked, and spendable-balance math subtracts delegated_vesting from
// the locked amount. Nobody is staked on the new chain, so leaving these set
// would understate what is locked and let vesting funds be spent early. It would
// also leave TrackUndelegation able to underflow with no delegations to undo.
func (s *spoon) zeroVestingDelegations() {
	for _, account := range s.accounts {
		base := baseVesting(account)
		if base == nil {
			continue
		}
		s.report.VestingAccounts++
		if !base.DelegatedVesting.IsZero() || !base.DelegatedFree.IsZero() {
			s.report.VestingZeroed++
		}
		base.DelegatedVesting = nil
		base.DelegatedFree = nil
	}
}

// flattenVestedAccounts replaces vesting accounts whose schedule has already
// elapsed by the genesis time with plain base accounts.
//
// A schedule that ended before the new chain starts locks nothing: once block
// time passes end_time, GetVestedCoins returns the whole original vesting
// amount, so the balance is entirely spendable either way and a base account is
// the same account with less to go wrong.
//
// It also removes state a genesis file cannot legally hold. mocha-4 carries two
// ContinuousVestingAccounts whose start_time is after their end_time, which
// MsgCreateVestingAccount permits (it only checks end_time > 0) but
// ContinuousVestingAccount.Validate rejects, so carrying them would make the new
// genesis fail ValidateGenesis and take `genesis gentx` and `collect-gentxs`
// down with it. Both ended years ago, so flattening is what makes them
// representable without changing what anyone can spend.
//
// Permanently locked accounts are never flattened: they have no end time and
// never vest. Neither is a malformed schedule that has not elapsed, because
// flattening that would unlock tokens early; it is left for the genesis
// validation in assemble to reject rather than silently decided here.
func (s *spoon) flattenVestedAccounts() {
	genesisTime := s.opts.GenesisTime.Unix()

	for i, account := range s.accounts {
		if _, permanent := account.(*vestingtypes.PermanentLockedAccount); permanent {
			continue
		}
		base := baseVesting(account)
		if base == nil || base.EndTime == 0 || base.EndTime > genesisTime {
			continue
		}
		s.accounts[i] = base.BaseAccount
		s.report.VestingFlattened++
	}
}

// prune drops accounts that hold nothing.
//
// Once sequences are reset there is nothing left worth preserving about an empty
// account, and it is recreated automatically the next time it receives funds.
// This runs after crediting so an account that just got its stake back is never
// dropped.
//
// Vesting accounts are always kept: their schedule is state that cannot be
// reconstructed, and an empty one still has to reject early spends.
func (s *spoon) prune() {
	if s.opts.NoPrune {
		return
	}

	kept := make([]sdk.AccountI, 0, len(s.accounts))
	for _, account := range s.accounts {
		address := account.GetAddress().String()
		if !s.balances[address].IsZero() || baseVesting(account) != nil {
			kept = append(kept, account)
			continue
		}
		delete(s.balances, address)
		s.report.Pruned++
	}
	s.accounts = kept
}

// adoptOrphanBalances creates accounts for balances that have none.
//
// The mocha-4 export has six such addresses. bank's InitGenesis is happy to
// write a balance with no matching account, but the resulting state is
// inconsistent and every parity check would flag it, so give them plain
// accounts instead of dropping funds.
func (s *spoon) adoptOrphanBalances() {
	have := make(map[string]struct{}, len(s.accounts))
	for _, account := range s.accounts {
		have[account.GetAddress().String()] = struct{}{}
	}

	orphans := make([]string, 0)
	for address, coins := range s.balances {
		if _, ok := have[address]; ok || coins.IsZero() {
			continue
		}
		orphans = append(orphans, address)
	}
	sort.Strings(orphans)

	for _, address := range orphans {
		accAddress, err := sdk.AccAddressFromBech32(address)
		if err != nil {
			// A balance under an unparseable address cannot be represented as
			// an account; leave it for Verify to reject.
			continue
		}
		s.accounts = append(s.accounts, authtypes.NewBaseAccount(accAddress, nil, 0, 0))
		s.report.Adopted = append(s.report.Adopted, address)
	}
}

// injectReserves creates new accounts funded out of thin air.
func (s *spoon) injectReserves() error {
	existing := make(map[string]struct{}, len(s.accounts))
	for _, account := range s.accounts {
		existing[account.GetAddress().String()] = struct{}{}
	}

	for _, reserve := range s.opts.Reserves {
		address, err := sdk.AccAddressFromBech32(reserve.Address)
		if err != nil {
			return fmt.Errorf("reserve address %q: %w", reserve.Address, err)
		}
		if _, ok := existing[reserve.Address]; ok {
			return fmt.Errorf(
				"reserve address %s already exists in the export; reserves must be fresh accounts "+
					"so their balance is unambiguous",
				reserve.Address,
			)
		}
		coins, err := sdk.ParseCoinsNormalized(reserve.Amount)
		if err != nil {
			return fmt.Errorf("reserve amount %q for %s: %w", reserve.Amount, reserve.Address, err)
		}
		if coins.IsZero() {
			return fmt.Errorf("reserve %s has a zero amount", reserve.Address)
		}

		existing[reserve.Address] = struct{}{}
		s.accounts = append(s.accounts, authtypes.NewBaseAccount(address, nil, 0, 0))
		s.balances[reserve.Address] = coins
		s.report.Reserves = s.report.Reserves.Add(coins...)
	}
	return nil
}

// normalizeAccounts resets sequences, renumbers accounts contiguously and drops
// public keys.
//
// Sequences reset to zero because the chain id is part of the signing bytes, so
// no old signature can be replayed here, and because it lets every gentx be
// signed with default flags. Account numbers are renumbered contiguously in the
// export's original order: auth's InitGenesis advances a global counter one step
// at a time until it reaches each account's number, so contiguous numbering is
// also the cheapest to load.
//
// Public keys are dropped unless asked otherwise. On a mocha-sized chain 73% of
// accounts carry one and they account for roughly 13 MiB of a genesis that has
// to fit under a hard wire limit. Nothing is lost: the ante handler stores an
// account's public key again from the first transaction it signs, and an account
// that never signs never needed one.
func (s *spoon) normalizeAccounts() {
	sort.SliceStable(s.accounts, func(i, j int) bool {
		return s.accounts[i].GetAccountNumber() < s.accounts[j].GetAccountNumber()
	})

	for i, account := range s.accounts {
		if err := account.SetSequence(0); err != nil {
			panic(fmt.Errorf("resetting sequence for %s: %w", account.GetAddress(), err))
		}
		if err := account.SetAccountNumber(uint64(i)); err != nil {
			panic(fmt.Errorf("renumbering %s: %w", account.GetAddress(), err))
		}
		if s.opts.KeepPubKeys {
			if account.GetPubKey() != nil {
				s.report.PubKeysKept++
			}
			continue
		}
		if account.GetPubKey() == nil {
			continue
		}
		if err := account.SetPubKey(nil); err != nil {
			panic(fmt.Errorf("clearing public key for %s: %w", account.GetAddress(), err))
		}
		s.report.PubKeysStripped++
	}
}

// moduleAccountBalance returns what a named module account holds, or nothing if
// the export has no such account.
func (s *spoon) moduleAccountBalance(name string) sdk.Coins {
	for _, account := range s.accounts {
		if module, ok := account.(*authtypes.ModuleAccount); ok && module.Name == name {
			return s.balances[module.GetAddress().String()]
		}
	}
	return sdk.NewCoins()
}

// baseVesting returns the embedded BaseVestingAccount of any vesting account
// type, or nil for everything else.
func baseVesting(account sdk.AccountI) *vestingtypes.BaseVestingAccount {
	switch typed := account.(type) {
	case *vestingtypes.ContinuousVestingAccount:
		return typed.BaseVestingAccount
	case *vestingtypes.DelayedVestingAccount:
		return typed.BaseVestingAccount
	case *vestingtypes.PeriodicVestingAccount:
		return typed.BaseVestingAccount
	case *vestingtypes.PermanentLockedAccount:
		return typed.BaseVestingAccount
	case *vestingtypes.BaseVestingAccount:
		return typed
	default:
		return nil
	}
}
