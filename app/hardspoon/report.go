package hardspoon

import (
	"fmt"
	"sort"
	"strings"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Report accounts for everything a spoon did, so that the supply on the new
// chain can be reconciled against the old one line by line.
type Report struct {
	ChainID string `json:"chain_id"`

	In  Totals `json:"in"`
	Out Totals `json:"out"`

	Credited Credited `json:"credited"`

	// Vaporized lists every balance that was dropped, with the reason.
	Vaporized []Vaporized `json:"vaporized"`

	EscrowChannels int       `json:"escrow_channels"`
	EscrowHeld     sdk.Coins `json:"escrow_held"`

	// OrphanedDenoms are the bridged-asset coins dropped from balances:
	// synthetic warp tokens whose routers, and IBC vouchers whose channels, do
	// not survive the spoon, so the coins could no longer be redeemed.
	OrphanedDenoms []OrphanedDenom `json:"orphaned_denoms,omitempty"`

	VestingAccounts int `json:"vesting_accounts"`
	VestingZeroed   int `json:"vesting_zeroed"`
	// VestingFlattened counts schedules that had already elapsed at the genesis
	// time and became plain base accounts.
	VestingFlattened int `json:"vesting_flattened"`

	// DenomMetadataDropped lists the base denoms of metadata entries that bank's
	// own validation rejects, which a genesis file cannot carry, or that name an
	// orphaned denom.
	DenomMetadataDropped []string `json:"denom_metadata_dropped,omitempty"`

	// GovExpeditedVotingPeriod records the expedited voting period being shortened
	// so it is strictly less than the regular one. Nil when the carried params
	// already satisfied that.
	GovExpeditedVotingPeriod *PeriodAdjustment `json:"gov_expedited_voting_period,omitempty"`

	Pruned          int      `json:"pruned"`
	Adopted         []string `json:"adopted"`
	PubKeysStripped int      `json:"pubkeys_stripped"`
	PubKeysKept     int      `json:"pubkeys_kept"`

	Reserves sdk.Coins `json:"reserves"`

	SizeBytes    int `json:"size_bytes"`
	MaxSizeBytes int `json:"max_size_bytes"`
}

// Totals is the account and supply position at one end of a spoon.
type Totals struct {
	Accounts int       `json:"accounts"`
	Balances int       `json:"balances"`
	Supply   sdk.Coins `json:"supply"`
}

// Credited is what was returned to accounts from staking.
type Credited struct {
	Delegations      int      `json:"delegations"`
	Principal        math.Int `json:"principal"`
	UnbondingEntries int      `json:"unbonding_entries"`
	Unbonding        math.Int `json:"unbonding"`
	Redelegations    int      `json:"redelegations"`
	// Staked is what the staking module says is staked: every validator's tokens
	// plus every unbonding delegation entry. This is what the credited total is
	// reconciled against.
	Staked math.Int `json:"staked"`
	// Pools is what the two staking pool module accounts held.
	Pools math.Int `json:"pools"`
	// Dust is Staked minus the credited total, lost to truncating each
	// delegation's stake.
	Dust math.Int `json:"dust"`
	// Surpluses are pool balances not backed by any validator. They are written
	// off with the rest of the module balance.
	Surpluses []Surplus `json:"surpluses,omitempty"`
}

// Surplus is a staking pool holding more than its validators account for.
type Surplus struct {
	Pool    string   `json:"pool"`
	Derived math.Int `json:"derived"`
	Held    math.Int `json:"held"`
	Surplus math.Int `json:"surplus"`
}

// PeriodAdjustment is a duration parameter that had to move for the genesis to be
// valid, recorded so the deviation from the old chain is never silent.
type PeriodAdjustment struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Vaporized is one dropped balance.
type Vaporized struct {
	Address  string    `json:"address"`
	Category string    `json:"category"`
	Coins    sdk.Coins `json:"coins"`
}

// OrphanedDenom is one bridged-asset denom dropped from every balance holding it.
type OrphanedDenom struct {
	Denom   string   `json:"denom"`
	Holders int      `json:"holders"`
	Amount  math.Int `json:"amount"`
}

// String renders the report for a terminal.
func (r *Report) String() string {
	var b strings.Builder

	fmt.Fprintf(&b, "hardspoon report for %s\n\n", r.ChainID)

	fmt.Fprintf(&b, "  accounts   %d in -> %d out\n", r.In.Accounts, r.Out.Accounts)
	fmt.Fprintf(&b, "  balances   %d in -> %d out\n", r.In.Balances, r.Out.Balances)

	fmt.Fprintf(&b, "\n  staking returned to accounts\n")
	fmt.Fprintf(&b, "    %-28s %d delegations, %s\n", "delegation principal", r.Credited.Delegations, r.Credited.Principal)
	fmt.Fprintf(&b, "    %-28s %d entries, %s\n", "unbonding delegations", r.Credited.UnbondingEntries, r.Credited.Unbonding)
	fmt.Fprintf(&b, "    %-28s %d (already counted in principal)\n", "redelegations", r.Credited.Redelegations)
	fmt.Fprintf(&b, "    %-28s %s\n", "staked per staking module", r.Credited.Staked)
	fmt.Fprintf(&b, "    %-28s %s (max %d)\n", "truncation dust", r.Credited.Dust, r.Credited.Delegations)
	fmt.Fprintf(&b, "    %-28s %s\n", "pool balances held", r.Credited.Pools)
	for _, surplus := range r.Credited.Surpluses {
		fmt.Fprintf(&b, "    %-28s %s in %s\n", "pool surplus", surplus.Surplus, surplus.Pool)
		fmt.Fprintf(&b, "    %-28s (not backed by any validator; written off)\n", "")
	}

	fmt.Fprintf(&b, "\n  dropped balances\n")
	if len(r.Vaporized) == 0 {
		fmt.Fprintf(&b, "    none\n")
	}
	for _, category := range r.vaporizedByCategory() {
		fmt.Fprintf(&b, "    %-28s %d accounts, %s\n", category.name, category.count, category.coins)
	}

	fmt.Fprintf(&b, "\n  ibc escrow\n")
	fmt.Fprintf(&b, "    %-28s %d\n", "transfer channels", r.EscrowChannels)
	fmt.Fprintf(&b, "    %-28s %s (matches transfer.total_escrowed)\n", "escrowed and dropped", r.EscrowHeld)

	if len(r.OrphanedDenoms) > 0 {
		fmt.Fprintf(&b, "\n  orphaned denoms (redemption paths do not survive the spoon; dropped)\n")
		for _, entry := range r.OrphanedDenoms {
			fmt.Fprintf(&b, "    %s\n", entry.Denom)
			fmt.Fprintf(&b, "    %-28s %s across %d holder(s)\n", "", entry.Amount, entry.Holders)
		}
	}

	fmt.Fprintf(&b, "\n  accounts\n")
	fmt.Fprintf(&b, "    %-28s %d (%d had delegation tracking cleared)\n", "vesting", r.VestingAccounts, r.VestingZeroed)
	if r.VestingFlattened > 0 {
		fmt.Fprintf(&b, "    %-28s %d (schedule elapsed; now base accounts)\n", "vesting flattened", r.VestingFlattened)
	}
	fmt.Fprintf(&b, "    %-28s %d\n", "pruned (held nothing)", r.Pruned)
	fmt.Fprintf(&b, "    %-28s %d\n", "adopted (balance, no account)", len(r.Adopted))
	if r.PubKeysStripped > 0 {
		fmt.Fprintf(&b, "    %-28s %d\n", "public keys stripped", r.PubKeysStripped)
	}
	if r.PubKeysKept > 0 {
		fmt.Fprintf(&b, "    %-28s %d\n", "public keys kept", r.PubKeysKept)
	}
	if !r.Reserves.IsZero() {
		fmt.Fprintf(&b, "    %-28s %s\n", "reserves added", r.Reserves)
	}

	if adjusted := r.GovExpeditedVotingPeriod; adjusted != nil {
		fmt.Fprintf(&b, "\n  gov\n")
		fmt.Fprintf(&b, "    %-28s %s -> %s\n", "expedited voting period", adjusted.From, adjusted.To)
		fmt.Fprintf(&b, "    %-28s (carried value was not strictly shorter than the voting period)\n", "")
	}

	if len(r.DenomMetadataDropped) > 0 {
		fmt.Fprintf(&b, "\n  bank\n")
		fmt.Fprintf(&b, "    %-28s %d (invalid for a genesis file, or orphaned)\n", "denom metadata dropped", len(r.DenomMetadataDropped))
		for _, base := range r.DenomMetadataDropped {
			fmt.Fprintf(&b, "    %-28s %s\n", "", base)
		}
	}

	fmt.Fprintf(&b, "\n  supply\n")
	fmt.Fprintf(&b, "    %-28s %s\n", "in", r.In.Supply)
	fmt.Fprintf(&b, "    %-28s %s\n", "out", r.Out.Supply)

	fmt.Fprintf(&b, "\n  size\n")
	fmt.Fprintf(&b, "    %-28s %d bytes (%.2f MiB)\n", "serialized", r.SizeBytes, float64(r.SizeBytes)/(1<<20))
	fmt.Fprintf(&b, "    %-28s %d bytes (%.2f MiB)\n", "cap", r.MaxSizeBytes, float64(r.MaxSizeBytes)/(1<<20))
	fmt.Fprintf(&b, "    %-28s %.2f MiB\n", "headroom", float64(r.MaxSizeBytes-r.SizeBytes)/(1<<20))
	if r.SizeBytes > WarnSizeBytes {
		fmt.Fprintf(&b, "\n  WARNING: above %d MiB. Re-measure at freeze.\n", WarnSizeBytes>>20)
		if r.PubKeysKept > 0 {
			fmt.Fprintf(&b, "  Dropping the %d retained public keys would recover several MiB.\n", r.PubKeysKept)
		}
	}

	return b.String()
}

type vaporizedCategory struct {
	name  string
	count int
	coins sdk.Coins
}

func (r *Report) vaporizedByCategory() []vaporizedCategory {
	index := make(map[string]*vaporizedCategory)
	for _, entry := range r.Vaporized {
		category, ok := index[entry.Category]
		if !ok {
			category = &vaporizedCategory{name: entry.Category, coins: sdk.NewCoins()}
			index[entry.Category] = category
		}
		category.count++
		category.coins = category.coins.Add(entry.Coins...)
	}

	categories := make([]vaporizedCategory, 0, len(index))
	for _, category := range index {
		categories = append(categories, *category)
	}
	sort.Slice(categories, func(i, j int) bool { return categories[i].name < categories[j].name })
	return categories
}
