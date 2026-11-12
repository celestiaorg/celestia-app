package genesis

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/app/params"
	minttypes "github.com/celestiaorg/celestia-app/v11/x/mint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"
)

const (
	stepRecords      = "records"
	stepConservation = "conservation"
)

var (
	accCodec = addresscodec.NewBech32Codec(params.Bech32PrefixAccAddr)
	valCodec = addresscodec.NewBech32Codec(params.Bech32PrefixValAddr)
)

// CheckResult holds, for the report, the signed annual_provisions - inflation_rate * supply[bond_denom] and the exported mint values.
type CheckResult struct {
	MintExcess       math.LegacyDec
	AnnualProvisions math.LegacyDec
	InflationRate    math.LegacyDec
	MintGenesisTime  time.Time
}

// CheckState runs the record-level rules and then the conservation equations on g.
func CheckState(cdc codec.JSONCodec, g *Genesis, opts Options) (*CheckResult, error) {
	pools := []string{stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName, distrtypes.ModuleName}
	supply, bals, err := checkBank(cdc, g, pools)
	if err != nil {
		return nil, err
	}
	bondDenom, err := checkStaking(cdc, g, bals)
	if err != nil {
		return nil, err
	}
	if err := checkDistribution(cdc, g, bals[distrtypes.ModuleName]); err != nil {
		return nil, err
	}
	return checkMint(cdc, g, bondDenom, supply, opts.AcceptMintExcess)
}

func decodeModule(cdc codec.JSONCodec, g *Genesis, name string, gs proto.Message) error {
	raw, ok := g.Module(name)
	if !ok {
		return &CheckError{Step: stepRecords, Module: name, Want: "present", Got: "missing"}
	}
	if err := cdc.UnmarshalJSON(raw, gs); err != nil {
		return &CheckError{Step: stepRecords, Module: name, Got: err.Error()}
	}
	return nil
}

// decodeAddr decodes records[i].field; with seen set, it rejects a key that an earlier index decoded to, as import would overwrite it.
func decodeAddr(ac address.Codec, seen map[string]int, module, records string, i int, field, addr string) (string, error) {
	bz, err := ac.StringToBytes(addr)
	if err != nil {
		return "", &CheckError{Step: stepRecords, Module: module, Field: fmt.Sprintf("%s[%d].%s", records, i, field), Want: "a valid address", Got: err.Error()}
	}
	if seen == nil {
		return string(bz), nil
	}
	if j, ok := seen[string(bz)]; ok {
		return "", duplicateError(module, records, i, j, addr)
	}
	seen[string(bz)] = i
	return string(bz), nil
}

func duplicateError(module, records string, i, j int, shown string) error {
	return &CheckError{Step: stepRecords, Module: module, Field: fmt.Sprintf("%s[%d]", records, i), Want: "a distinct key", Got: fmt.Sprintf("%s, the key of %s[%d]", shown, records, j)}
}

// checkBank applies the balance identity rules, checks supply == sum(balances) and returns the supply and the balances of modules.
func checkBank(cdc codec.JSONCodec, g *Genesis, modules []string) (sdk.Coins, map[string]sdk.Coins, error) {
	var gs banktypes.GenesisState
	if err := decodeModule(cdc, g, banktypes.ModuleName, &gs); err != nil {
		return nil, nil, err
	}
	byAddr := make(map[string]string, len(modules))
	for _, name := range modules {
		byAddr[string(authtypes.NewModuleAddress(name))] = name
	}
	bals := make(map[string]sdk.Coins, len(modules))
	seen := make(map[string]int, len(gs.Balances))
	for i, b := range gs.Balances {
		field := fmt.Sprintf("balances[%d]", i)
		addr, err := decodeAddr(accCodec, seen, banktypes.ModuleName, "balances", i, "address", b.Address)
		if err != nil {
			return nil, nil, err
		}
		if err := validCoins(b.Coins); err != nil {
			return nil, nil, &CheckError{Step: stepRecords, Module: banktypes.ModuleName, Field: field + ".coins", Want: "valid positive coins", Got: err.Error()}
		}
		if name, ok := byAddr[addr]; ok {
			bals[name] = b.Coins
		}
	}
	if err := validCoins(gs.Supply); err != nil {
		return nil, nil, &CheckError{Step: stepRecords, Module: banktypes.ModuleName, Field: "supply", Want: "valid positive coins", Got: err.Error()}
	}

	// InitGenesis accepts an empty supply and recomputes it; the tool does not.
	if gs.Supply.Empty() {
		return nil, nil, &CheckError{Step: stepConservation, Module: banktypes.ModuleName, Field: "supply", Want: "non-empty", Got: "empty"}
	}
	sum := intSum{}
	for _, b := range gs.Balances {
		if err := sum.addCoins(b.Coins); err != nil {
			return nil, nil, overflowError(banktypes.ModuleName, "balances", err)
		}
	}
	if err := equalCoins(banktypes.ModuleName, "supply", sum.coins(), gs.Supply); err != nil {
		return nil, nil, err
	}
	return gs.Supply, bals, nil
}

// checkStaking checks validator and power records and the pool balances, and returns the bond denom.
func checkStaking(cdc codec.JSONCodec, g *Genesis, bals map[string]sdk.Coins) (string, error) {
	const module = stakingtypes.ModuleName
	var gs stakingtypes.GenesisState
	if err := decodeModule(cdc, g, module, &gs); err != nil {
		return "", err
	}
	// A non-exported genesis runs creation hooks that overwrite distribution records.
	if !gs.Exported {
		return "", &CheckError{Step: stepRecords, Module: module, Field: "exported", Want: "true", Got: "false"}
	}
	denom := gs.Params.BondDenom
	if err := sdk.ValidateDenom(denom); err != nil {
		return "", &CheckError{Step: stepRecords, Module: module, Field: "params.bond_denom", Want: "a valid denom", Got: err.Error()}
	}
	bonded, notBonded := intSum{}, intSum{}
	powers := make(map[string]int64) // bonded operator -> power from tokens
	seen := make(map[string]int, len(gs.Validators))
	for i, v := range gs.Validators {
		field := fmt.Sprintf("validators[%d]", i)
		op, err := decodeAddr(valCodec, seen, module, "validators", i, "operator_address", v.OperatorAddress)
		if err != nil {
			return "", err
		}
		if err := nonNegative(module, field+".tokens", v.Tokens); err != nil {
			return "", err
		}
		switch v.Status {
		case stakingtypes.Bonded:
			if v.Jailed {
				return "", &CheckError{Step: stepRecords, Module: module, Field: field + ".jailed", Want: "false for a bonded validator", Got: "true"}
			}
			// sdk.TokensToConsensusPower panics if the power does not fit in int64.
			power := v.Tokens.Quo(sdk.DefaultPowerReduction)
			if !power.IsInt64() {
				return "", &CheckError{Step: stepRecords, Module: module, Field: field + ".tokens", Want: "a power within int64", Got: v.Tokens.String()}
			}
			powers[op] = power.Int64()
			err = bonded.add(denom, v.Tokens)
		case stakingtypes.Unbonding, stakingtypes.Unbonded:
			err = notBonded.add(denom, v.Tokens)
		default:
			return "", &CheckError{Step: stepRecords, Module: module, Field: field + ".status", Want: "bonded, unbonding or unbonded", Got: v.Status.String()}
		}
		if err != nil {
			return "", overflowError(module, "validators", err)
		}
	}
	if err := checkPowers(&gs, powers); err != nil {
		return "", err
	}
	if err := addUBDs(&gs, notBonded); err != nil {
		return "", err
	}
	if err := equalCoins(module, stakingtypes.BondedPoolName, bonded.coins(), bals[stakingtypes.BondedPoolName]); err != nil {
		return "", err
	}
	if err := equalCoins(module, stakingtypes.NotBondedPoolName, notBonded.coins(), bals[stakingtypes.NotBondedPoolName]); err != nil {
		return "", err
	}
	return denom, nil
}

// addUBDs rejects duplicate (delegator, validator) pairs and adds every entry balance to notBonded.
func addUBDs(gs *stakingtypes.GenesisState, notBonded intSum) error {
	const module, records = stakingtypes.ModuleName, "unbonding_delegations"
	seen := make(map[[2]string]int, len(gs.UnbondingDelegations))
	for i, ubd := range gs.UnbondingDelegations {
		del, err := decodeAddr(accCodec, nil, module, records, i, "delegator_address", ubd.DelegatorAddress)
		if err != nil {
			return err
		}
		val, err := decodeAddr(valCodec, nil, module, records, i, "validator_address", ubd.ValidatorAddress)
		if err != nil {
			return err
		}
		if j, ok := seen[[2]string{del, val}]; ok {
			return duplicateError(module, records, i, j, ubd.DelegatorAddress)
		}
		seen[[2]string{del, val}] = i
		for j, e := range ubd.Entries {
			if err := nonNegative(module, fmt.Sprintf("%s[%d].entries[%d].balance", records, i, j), e.Balance); err != nil {
				return err
			}
			if err := notBonded.add(gs.Params.BondDenom, e.Balance); err != nil {
				return overflowError(module, records, err)
			}
		}
	}
	return nil
}

// checkPowers requires last_validator_powers to list exactly the bonded validators with their token power.
func checkPowers(gs *stakingtypes.GenesisState, powers map[string]int64) error {
	const module = stakingtypes.ModuleName
	lvp := gs.LastValidatorPowers
	if len(lvp) > int(gs.Params.MaxValidators) {
		return &CheckError{Step: stepRecords, Module: module, Field: "last_validator_powers", Want: fmt.Sprintf("<= %d entries (max_validators)", gs.Params.MaxValidators), Got: fmt.Sprint(len(lvp))}
	}
	total := math.ZeroInt()
	seen := make(map[string]int, len(lvp))
	for i, lv := range lvp {
		field := fmt.Sprintf("last_validator_powers[%d]", i)
		op, err := decodeAddr(valCodec, seen, module, "last_validator_powers", i, "address", lv.Address)
		if err != nil {
			return err
		}
		want, ok := powers[op]
		if !ok {
			return &CheckError{Step: stepRecords, Module: module, Field: field + ".address", Want: "a bonded validator", Got: lv.Address}
		}
		if lv.Power <= 0 {
			return &CheckError{Step: stepRecords, Module: module, Field: field + ".power", Want: "> 0", Got: fmt.Sprint(lv.Power)}
		}
		if lv.Power != want {
			return &CheckError{Step: stepConservation, Module: module, Field: field + ".power", Want: fmt.Sprint(want), Got: fmt.Sprint(lv.Power)}
		}
		if total, err = total.SafeAdd(math.NewInt(lv.Power)); err != nil {
			return overflowError(module, "last_validator_powers", err)
		}
	}
	if len(lvp) != len(powers) {
		return &CheckError{Step: stepConservation, Module: module, Field: "last_validator_powers", Want: fmt.Sprintf("%d entries, one per bonded validator", len(powers)), Got: fmt.Sprint(len(lvp))}
	}
	if err := nonNegative(module, "last_total_power", gs.LastTotalPower); err != nil {
		return err
	}
	if !gs.LastTotalPower.Equal(total) {
		return &CheckError{Step: stepConservation, Module: module, Field: "last_total_power", Want: total.String(), Got: gs.LastTotalPower.String()}
	}
	return nil
}

// checkDistribution compares the module balance with the holdings InitGenesis sums; commission is inside outstanding rewards.
func checkDistribution(cdc codec.JSONCodec, g *Genesis, bal sdk.Coins) error {
	const module = distrtypes.ModuleName
	var gs distrtypes.GenesisState
	if err := decodeModule(cdc, g, module, &gs); err != nil {
		return err
	}
	sum := decSum{whole: intSum{}, frac: map[string]math.LegacyDec{}}
	seen := make(map[string]int, len(gs.OutstandingRewards))
	for i, r := range gs.OutstandingRewards {
		if _, err := decodeAddr(valCodec, seen, module, "outstanding_rewards", i, "validator_address", r.ValidatorAddress); err != nil {
			return err
		}
		if err := validDecCoins(r.OutstandingRewards); err != nil {
			return &CheckError{Step: stepRecords, Module: module, Field: fmt.Sprintf("outstanding_rewards[%d]", i), Want: "valid positive coins", Got: err.Error()}
		}
		if err := sum.add(r.OutstandingRewards); err != nil {
			return overflowError(module, "outstanding_rewards", err)
		}
	}
	// Claimed rewards not yet sent (CIP-30) are held by the module too.
	if err := distrtypes.ValidateUserOutstandingRewards(gs.UserOutstandingRewards, accCodec, valCodec); err != nil {
		return &CheckError{Step: stepRecords, Module: module, Field: "user_outstanding_rewards", Want: "valid records", Got: err.Error()}
	}
	for _, r := range gs.UserOutstandingRewards {
		if err := sum.whole.addCoins(r.Rewards); err != nil {
			return overflowError(module, "user_outstanding_rewards", err)
		}
	}
	if err := validDecCoins(gs.FeePool.CommunityPool); err != nil {
		return &CheckError{Step: stepRecords, Module: module, Field: "fee_pool.community_pool", Want: "valid positive coins", Got: err.Error()}
	}
	if err := sum.add(gs.FeePool.CommunityPool); err != nil {
		return overflowError(module, "fee_pool.community_pool", err)
	}
	holdings, err := sum.truncate()
	if err != nil {
		return overflowError(module, "holdings", err)
	}
	return equalCoins(module, "balance", holdings, bal)
}

// checkMint checks the minter against staking and supply and returns its values with the signed excess.
func checkMint(cdc codec.JSONCodec, g *Genesis, bondDenom string, supply sdk.Coins, ack math.LegacyDec) (*CheckResult, error) {
	var gs minttypes.GenesisState
	if err := decodeModule(cdc, g, minttypes.ModuleName, &gs); err != nil {
		return nil, err
	}
	fail := func(field, want, got string) (*CheckResult, error) {
		return nil, &CheckError{Step: stepConservation, Module: minttypes.ModuleName, Field: field, Want: want, Got: got}
	}
	m := gs.Minter
	if m == nil || gs.GenesisTime == nil {
		return fail("minter and genesis_time", "set", "missing")
	}
	if gs.BondDenom != bondDenom || m.BondDenom != bondDenom {
		return fail("bond_denom", bondDenom+" as in staking", fmt.Sprintf("%s, minter %s", gs.BondDenom, m.BondDenom))
	}
	if m.InflationRate.IsNil() || m.AnnualProvisions.IsNil() {
		return fail("minter", "inflation_rate and annual_provisions set", "nil")
	}
	// These bounds keep the arithmetic below inside the LegacyDec range.
	if m.InflationRate.IsNegative() || m.InflationRate.GT(math.LegacyOneDec()) {
		return fail("minter.inflation_rate", "between 0 and 1", m.InflationRate.String())
	}
	if m.AnnualProvisions.IsNegative() {
		return fail("minter.annual_provisions", ">= 0", m.AnnualProvisions.String())
	}
	excess := m.AnnualProvisions.Sub(m.InflationRate.MulInt(supply.AmountOfNoDenomValidation(bondDenom)))
	if !ack.IsNil() && !ack.Equal(excess) {
		return fail("accept-mint-excess", excess.String(), ack.String())
	}
	// Provisions are recomputed only when the rate changes, so a burn since then gives a positive excess on honest state.
	if ack.IsNil() && excess.IsPositive() {
		return fail("excess", "<= 0 or acknowledged", excess.String())
	}
	return &CheckResult{MintExcess: excess, AnnualProvisions: m.AnnualProvisions, InflationRate: m.InflationRate, MintGenesisTime: *gs.GenesisTime}, nil
}

// validCoins and validDecCoins reject nil, zero, negative, unsorted, duplicate and invalid-denom coins.
func validCoins(coins sdk.Coins) error {
	for _, c := range coins {
		if c.Amount.IsNil() {
			return fmt.Errorf("%s: nil amount", c.Denom)
		}
	}
	return coins.Validate()
}

func validDecCoins(coins sdk.DecCoins) error {
	for _, c := range coins {
		if c.Amount.IsNil() {
			return fmt.Errorf("%s: nil amount", c.Denom)
		}
	}
	return coins.Validate()
}

func nonNegative(module, field string, amt math.Int) error {
	if amt.IsNil() || amt.IsNegative() {
		return &CheckError{Step: stepRecords, Module: module, Field: field, Want: ">= 0", Got: amt.String()}
	}
	return nil
}

func overflowError(module, field string, err error) error {
	return &CheckError{Step: stepConservation, Module: module, Field: field, Want: "a sum within 256 bits", Got: err.Error()}
}

// equalCoins fails on the first denom whose amount differs, over every denom of either sorted set.
func equalCoins(module, field string, want, got sdk.Coins) error {
	for _, denom := range append(want.Denoms(), got.Denoms()...) {
		w, g := want.AmountOfNoDenomValidation(denom), got.AmountOfNoDenomValidation(denom)
		if !w.Equal(g) {
			return &CheckError{Step: stepConservation, Module: module, Field: field + "[" + denom + "]", Want: w.String(), Got: g.String()}
		}
	}
	return nil
}

// intSum totals amounts per denom and errors instead of overflowing.
type intSum map[string]math.Int

func (s intSum) add(denom string, amt math.Int) (err error) {
	sum := amt
	if cur, ok := s[denom]; ok {
		sum, err = cur.SafeAdd(amt)
	}
	if err == nil {
		s[denom] = sum
	}
	return err
}

func (s intSum) addCoins(coins sdk.Coins) error {
	for _, c := range coins {
		if err := s.add(c.Denom, c.Amount); err != nil {
			return err
		}
	}
	return nil
}

// coins returns the non-zero totals sorted by denom.
func (s intSum) coins() sdk.Coins {
	coins := sdk.Coins{}
	for _, denom := range slices.Sorted(maps.Keys(s)) {
		if !s[denom].IsZero() {
			coins = append(coins, sdk.Coin{Denom: denom, Amount: s[denom]})
		}
	}
	return coins
}

// decSum totals non-negative decimals per denom as whole and fractional parts, so no addition overflows.
type decSum struct {
	whole intSum
	frac  map[string]math.LegacyDec
}

func (s decSum) add(coins sdk.DecCoins) error {
	for _, c := range coins {
		whole := c.Amount.TruncateInt()
		if err := s.whole.add(c.Denom, whole); err != nil {
			return err
		}
		frac := c.Amount.Sub(math.LegacyNewDecFromInt(whole))
		if cur, ok := s.frac[c.Denom]; ok {
			frac = cur.Add(frac)
		}
		s.frac[c.Denom] = frac
	}
	return nil
}

// truncate returns the whole part of each total; truncation happens only after aggregation.
func (s decSum) truncate() (sdk.Coins, error) {
	for _, denom := range slices.Sorted(maps.Keys(s.frac)) {
		if err := s.whole.add(denom, s.frac[denom].TruncateInt()); err != nil {
			return nil, err
		}
	}
	return s.whole.coins(), nil
}
