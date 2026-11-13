package genesis_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/pkg/hardfork/genesis"
	minttypes "github.com/celestiaorg/celestia-app/v11/x/mint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
)

type edit = func(t *testing.T, g *genutiltypes.AppGenesis)

func TestCheckState(t *testing.T) {
	f := newFixture(t)
	g := f.decode(t)
	bank := moduleOf[banktypes.GenesisState](t, f, g, banktypes.ModuleName)
	staking := moduleOf[stakingtypes.GenesisState](t, f, g, stakingtypes.ModuleName)
	distr := moduleOf[distrtypes.GenesisState](t, f, g, distrtypes.ModuleName)
	mint := moduleOf[minttypes.GenesisState](t, f, g, minttypes.ModuleName)
	pool := func(name string) math.Int {
		for _, b := range bank.Balances {
			if b.Address == authtypes.NewModuleAddress(name).String() {
				return b.Coins.AmountOf("utia")
			}
		}
		return math.ZeroInt()
	}
	supply, bonded, notBonded := bank.Supply.AmountOf("utia"), pool(stakingtypes.BondedPoolName), pool(stakingtypes.NotBondedPoolName)
	product := mint.Minter.InflationRate.MulInt(supply)
	excess := product.Add(math.LegacySmallestDec())
	acc, val := bank.Balances[0].Address, staking.Validators[0].OperatorAddress
	nVals, nPowers, nUBDs, nOutstanding := len(staking.Validators), len(staking.LastValidatorPowers), len(staking.UnbondingDelegations), len(distr.OutstandingRewards)
	maxInt := math.NewIntFromBigInt(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)))

	rec := func(module, field, want string) *genesis.CheckError {
		return &genesis.CheckError{Step: "records", Module: module, Field: field, Want: want}
	}
	cons := func(module, field, want string) *genesis.CheckError {
		return &genesis.CheckError{Step: "conservation", Module: module, Field: field, Want: want}
	}
	bankEdit := func(fn func(gs *banktypes.GenesisState)) edit { return editModule(f, banktypes.ModuleName, fn) }
	stakingEdit := func(fn func(gs *stakingtypes.GenesisState)) edit { return editModule(f, stakingtypes.ModuleName, fn) }
	distrEdit := func(fn func(gs *distrtypes.GenesisState)) edit { return editModule(f, distrtypes.ModuleName, fn) }
	mintEdit := func(fn func(gs *minttypes.GenesisState)) edit { return editModule(f, minttypes.ModuleName, fn) }
	credit := func(module string, c sdk.Coin) edit {
		return bankEdit(func(gs *banktypes.GenesisState) { addCoin(gs, authtypes.NewModuleAddress(module).String(), c) })
	}
	utia := func(n int64) sdk.Coin { return sdk.NewInt64Coin("utia", n) }
	foo := func(amount string) sdk.DecCoins {
		return sdk.DecCoins{sdk.NewDecCoinFromDec("foo", math.LegacyMustNewDecFromStr(amount))}
	}
	addUBD := func(dels ...string) edit {
		return stakingEdit(func(gs *stakingtypes.GenesisState) {
			for _, del := range dels {
				entries := []stakingtypes.UnbondingDelegationEntry{{InitialBalance: math.NewInt(5), Balance: math.NewInt(5)}}
				gs.UnbondingDelegations = append(gs.UnbondingDelegations, stakingtypes.UnbondingDelegation{DelegatorAddress: del, ValidatorAddress: val, Entries: entries})
			}
		})
	}
	addOutstanding := func(amount string, vals ...string) edit {
		return distrEdit(func(gs *distrtypes.GenesisState) {
			for _, v := range vals {
				gs.OutstandingRewards = append(gs.OutstandingRewards, distrtypes.ValidatorOutstandingRewardsRecord{ValidatorAddress: v, OutstandingRewards: foo(amount)})
			}
		})
	}
	setProvisions := func(ap math.LegacyDec) edit {
		return mintEdit(func(gs *minttypes.GenesisState) { gs.Minter.AnnualProvisions = ap })
	}
	val1, val2 := sdk.ValAddress(bytes.Repeat([]byte{1}, 20)).String(), sdk.ValAddress(bytes.Repeat([]byte{2}, 20)).String()

	testCases := []struct {
		name   string
		edit   edit
		ack    string              // AcceptMintExcess; empty means none
		want   *genesis.CheckError // nil means accepted
		excess string              // the MintExcess of an accepted case; empty means not checked
	}{
		{name: "fixture accepted"},
		// bank
		{name: "upper-case alias of an account", edit: bankEdit(func(gs *banktypes.GenesisState) { addCoin(gs, strings.ToUpper(acc), utia(1)) }), want: rec("bank", fmt.Sprintf("balances[%d]", len(bank.Balances)), "a distinct key")},
		{name: "invalid address", edit: bankEdit(func(gs *banktypes.GenesisState) { gs.Balances[0].Address = "celestia1invalid" }), want: rec("bank", "balances[0].address", "a valid address")},
		{name: "zero coin", edit: bankEdit(func(gs *banktypes.GenesisState) { gs.Balances[0].Coins[0].Amount = math.ZeroInt() }), want: rec("bank", "balances[0].coins", "valid positive coins")},
		{name: "nil coin amount", edit: rawEdit(banktypes.ModuleName, `,"amount":"`+bank.Balances[0].Coins[0].Amount.String()+`"`), want: rec("bank", "balances[0].coins", "valid positive coins")},
		{name: "empty supply", edit: bankEdit(func(gs *banktypes.GenesisState) { gs.Supply = nil }), want: cons("bank", "supply", "non-empty")},
		{name: "supply off by one utia", edit: bankEdit(func(gs *banktypes.GenesisState) { gs.Balances[0].Coins = gs.Balances[0].Coins.Add(utia(1)) }), want: cons("bank", "supply[utia]", supply.AddRaw(1).String())},
		{name: "extra denom in supply", edit: bankEdit(func(gs *banktypes.GenesisState) { gs.Supply = gs.Supply.Add(sdk.NewInt64Coin("foo", 1)) }), want: cons("bank", "supply[foo]", "0")},
		{name: "balance sum overflow", edit: bankEdit(func(gs *banktypes.GenesisState) {
			for _, b := range []byte{1, 2} {
				gs.Balances = append(gs.Balances, banktypes.Balance{Address: sdk.AccAddress(bytes.Repeat([]byte{b}, 20)).String(), Coins: sdk.NewCoins(sdk.NewCoin("foo", maxInt))})
			}
		}), want: cons("bank", "balances", "a sum within 256 bits")},
		// staking
		{name: "not exported", edit: stakingEdit(func(gs *stakingtypes.GenesisState) { gs.Exported = false }), want: rec("staking", "exported", "true")},
		{name: "upper-case alias of a validator", edit: stakingEdit(func(gs *stakingtypes.GenesisState) {
			v := gs.Validators[0]
			v.OperatorAddress = strings.ToUpper(v.OperatorAddress)
			gs.Validators = append(gs.Validators, v)
		}), want: rec("staking", fmt.Sprintf("validators[%d]", nVals), "a distinct key")},
		{name: "jailed bonded validator", edit: stakingEdit(func(gs *stakingtypes.GenesisState) { gs.Validators[0].Jailed = true }), want: rec("staking", "validators[0].jailed", "false for a bonded validator")},
		{name: "power off by one", edit: stakingEdit(func(gs *stakingtypes.GenesisState) { gs.LastValidatorPowers[0].Power++ }), want: cons("staking", "last_validator_powers[0].power", fmt.Sprint(staking.LastValidatorPowers[0].Power))},
		{name: "zero power", edit: stakingEdit(func(gs *stakingtypes.GenesisState) { gs.LastValidatorPowers[0].Power = 0 }), want: rec("staking", "last_validator_powers[0].power", "> 0")},
		{name: "bonded validator without power", edit: stakingEdit(func(gs *stakingtypes.GenesisState) { gs.LastValidatorPowers = nil }), want: cons("staking", "last_validator_powers", fmt.Sprintf("%d entries, one per bonded validator", nPowers))},
		{name: "power of an unknown validator", edit: stakingEdit(func(gs *stakingtypes.GenesisState) {
			gs.LastValidatorPowers = append(gs.LastValidatorPowers, stakingtypes.LastValidatorPower{Address: val1, Power: 1})
		}), want: rec("staking", fmt.Sprintf("last_validator_powers[%d].address", nPowers), "a bonded validator")},
		{name: "upper-case alias of a power", edit: stakingEdit(func(gs *stakingtypes.GenesisState) {
			lv := gs.LastValidatorPowers[0]
			lv.Address = strings.ToUpper(lv.Address)
			gs.LastValidatorPowers = append(gs.LastValidatorPowers, lv)
		}), want: rec("staking", fmt.Sprintf("last_validator_powers[%d]", nPowers), "a distinct key")},
		{name: "total power off by one", edit: stakingEdit(func(gs *stakingtypes.GenesisState) { gs.LastTotalPower = gs.LastTotalPower.AddRaw(1) }), want: cons("staking", "last_total_power", staking.LastTotalPower.String())},
		{name: "bonded pool off by one", edit: credit(stakingtypes.BondedPoolName, utia(1)), want: cons("staking", "bonded_tokens_pool[utia]", bonded.String())},
		{name: "not bonded pool missing an unbonding entry", edit: addUBD(acc), want: cons("staking", "not_bonded_tokens_pool[utia]", notBonded.AddRaw(5).String())},
		{name: "unbonding entry counted", edit: both(addUBD(acc), credit(stakingtypes.NotBondedPoolName, utia(5)))},
		{name: "upper-case alias of an unbonding delegation", edit: addUBD(acc, strings.ToUpper(acc)), want: rec("staking", fmt.Sprintf("unbonding_delegations[%d]", nUBDs+1), "a distinct key")},
		// distribution
		{name: "distribution off by one after truncation", edit: both(distrEdit(func(gs *distrtypes.GenesisState) {
			gs.FeePool.CommunityPool = gs.FeePool.CommunityPool.Add(foo("1.5")...)
		}), credit(distrtypes.ModuleName, sdk.NewInt64Coin("foo", 2))), want: cons("distribution", "balance[foo]", "1")},
		{name: "distribution dust accepted", edit: distrEdit(func(gs *distrtypes.GenesisState) {
			gs.FeePool.CommunityPool = gs.FeePool.CommunityPool.Add(foo("0.5")...)
		})},
		{name: "distribution truncates after aggregation", edit: both(addOutstanding("0.6", val1, val2), credit(distrtypes.ModuleName, sdk.NewInt64Coin("foo", 1)))},
		{name: "upper-case alias of outstanding rewards", edit: addOutstanding("1", val1, strings.ToUpper(val1)), want: rec("distribution", fmt.Sprintf("outstanding_rewards[%d]", nOutstanding+1), "a distinct key")},
		{name: "distribution counts a CIP-30 entry", edit: distrEdit(func(gs *distrtypes.GenesisState) {
			gs.UserOutstandingRewards = append(gs.UserOutstandingRewards, distrtypes.UserOutstandingRewardsRecord{DelegatorAddress: acc, ValidatorAddress: val, Rewards: sdk.NewCoins(sdk.NewInt64Coin("foo", 3))})
		}), want: cons("distribution", "balance[foo]", "3")},
		{name: "distribution does not add commission again", edit: distrEdit(func(gs *distrtypes.GenesisState) {
			gs.ValidatorAccumulatedCommissions = append(gs.ValidatorAccumulatedCommissions, distrtypes.ValidatorAccumulatedCommissionRecord{ValidatorAddress: val1, Accumulated: distrtypes.ValidatorAccumulatedCommission{Commission: foo("7")}})
		})},
		// mint
		{name: "mint bond denom differs from staking", edit: mintEdit(func(gs *minttypes.GenesisState) { gs.BondDenom = "foo" }), want: cons("mint", "bond_denom", "utia as in staking")},
		{name: "mint nil annual provisions", edit: rawEdit(minttypes.ModuleName, `,"annual_provisions":"`+mint.Minter.AnnualProvisions.String()+`"`), want: cons("mint", "minter", "inflation_rate and annual_provisions set")},
		{name: "mint inflation rate above 1", edit: mintEdit(func(gs *minttypes.GenesisState) { gs.Minter.InflationRate = math.LegacyNewDec(2) }), want: cons("mint", "minter.inflation_rate", "between 0 and 1")},
		{name: "mint negative baseline returned signed", edit: setProvisions(math.LegacyZeroDec()), excess: product.Neg().String()},
		{name: "mint positive excess", edit: setProvisions(excess), want: cons("mint", "excess", "<= 0 or acknowledged")},
		{name: "mint excess acknowledged exactly", edit: setProvisions(excess), ack: "0.000000000000000001", excess: "0.000000000000000001"},
		{name: "mint excess acknowledged off by 1e-18", edit: setProvisions(excess), ack: "0.000000000000000002", want: cons("mint", "accept-mint-excess", "0.000000000000000001")},
		{name: "mint acknowledgment with a negative excess", edit: setProvisions(math.LegacyZeroDec()), ack: "0", want: cons("mint", "accept-mint-excess", product.Neg().String())},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := f.decode(t)
			if tc.edit != nil {
				tc.edit(t, g)
			}
			file := fixture{cdc: f.cdc, modules: f.modules, file: encode(t, g)}
			opts := file.options()
			exp, err := genesis.LoadExport(bytes.NewReader(file.file), f.modules, opts)
			require.NoError(t, err)
			if tc.ack != "" {
				opts.AcceptMintExcess = math.LegacyMustNewDecFromStr(tc.ack)
			}

			res, err := genesis.CheckState(f.cdc, &exp.Genesis, opts)
			if tc.want != nil {
				requireCheckError(t, err, *tc.want, "")
				return
			}
			require.NoError(t, err)
			if tc.excess != "" {
				require.Equal(t, tc.excess, res.MintExcess.String())
			}
			require.True(t, res.InflationRate.Equal(mint.Minter.InflationRate))
			require.True(t, res.MintGenesisTime.Equal(*mint.GenesisTime))
		})
	}
}

// moduleOf decodes the named module of g.
func moduleOf[T any, P interface {
	*T
	proto.Message
}](t *testing.T, f fixture, g *genutiltypes.AppGenesis, name string) P {
	t.Helper()
	var state map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(g.AppState, &state))
	gs := P(new(T))
	require.NoError(t, f.cdc.UnmarshalJSON(state[name], gs))
	return gs
}

// editModule returns an edit that applies fn to the named module.
func editModule[T any, P interface {
	*T
	proto.Message
}](f fixture, name string, fn func(P)) edit {
	return func(t *testing.T, g *genutiltypes.AppGenesis) {
		gs := moduleOf[T, P](t, f, g, name)
		fn(gs)
		raw, err := f.cdc.MarshalJSON(gs)
		require.NoError(t, err)
		setModule(t, g, name, raw)
	}
}

// rawEdit returns an edit that removes the first cut from the named module's JSON; the codec cannot write a nil amount.
func rawEdit(name, cut string) edit {
	return func(t *testing.T, g *genutiltypes.AppGenesis) {
		var state map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(g.AppState, &state))
		require.Contains(t, string(state[name]), cut)
		setModule(t, g, name, []byte(strings.Replace(string(state[name]), cut, "", 1)))
	}
}

func setModule(t *testing.T, g *genutiltypes.AppGenesis, name string, raw []byte) {
	t.Helper()
	var state map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(g.AppState, &state))
	state[name] = raw
	var err error
	g.AppState, err = json.Marshal(state)
	require.NoError(t, err)
}

func both(a, b edit) edit {
	return func(t *testing.T, g *genutiltypes.AppGenesis) { a(t, g); b(t, g) }
}

// addCoin adds c to the balance of addr, creating it if needed, and to supply.
func addCoin(gs *banktypes.GenesisState, addr string, c sdk.Coin) {
	gs.Supply = gs.Supply.Add(c)
	for i := range gs.Balances {
		if gs.Balances[i].Address == addr {
			gs.Balances[i].Coins = gs.Balances[i].Coins.Add(c)
			return
		}
	}
	gs.Balances = append(gs.Balances, banktypes.Balance{Address: addr, Coins: sdk.NewCoins(c)})
}
