package genesis_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/math"
	hyputil "github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/hardfork/genesis"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	minttypes "github.com/celestiaorg/celestia-app/v11/x/mint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
	ibccoretypes "github.com/cosmos/ibc-go/v8/modules/core/types"
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
	fund := func(addr sdk.AccAddress, c sdk.Coin) edit {
		return bankEdit(func(gs *banktypes.GenesisState) { addCoin(gs, addr.String(), c) })
	}
	credit := func(module string, c sdk.Coin) edit { return fund(authtypes.NewModuleAddress(module), c) }
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
	blockTime := f.options().ExportBlockTime
	afterT, notAfterT := blockTime.Add(time.Nanosecond), "<= export-block-time "+formatTime(blockTime)
	govEdit := func(fn func(gs *govv1.GenesisState)) edit { return editModule(f, govtypes.ModuleName, fn) }
	deposit := func(depositors ...string) edit {
		return govEdit(func(gs *govv1.GenesisState) {
			for _, d := range depositors {
				gs.Deposits = append(gs.Deposits, &govv1.Deposit{ProposalId: 1, Depositor: d, Amount: sdk.NewCoins(utia(5))})
			}
		})
	}
	vote := func(weight string, voters ...string) edit {
		return govEdit(func(gs *govv1.GenesisState) {
			for _, v := range voters {
				gs.Votes = append(gs.Votes, &govv1.Vote{ProposalId: 1, Voter: v, Options: []*govv1.WeightedVoteOption{{Option: govv1.OptionYes, Weight: weight}}})
			}
		})
	}
	// proposal adds proposal 1 in its voting period and moves starting_proposal_id past it.
	proposal := func(end *time.Time) edit {
		return govEdit(func(gs *govv1.GenesisState) {
			gs.Proposals = append(gs.Proposals, &govv1.Proposal{Id: 1, Status: govv1.StatusVotingPeriod, SubmitTime: &blockTime, VotingEndTime: end})
			gs.StartingProposalId = 2
		})
	}
	fibreEdit := func(fn func(gs *fibretypes.GenesisState)) edit { return editModule(f, fibretypes.ModuleName, fn) }
	escrow := func(signer string, balance, available, pending int64) edit {
		return fibreEdit(func(gs *fibretypes.GenesisState) {
			gs.EscrowAccounts = append(gs.EscrowAccounts, fibretypes.EscrowAccount{Signer: signer, Balance: utia(balance), AvailableBalance: utia(available)})
			if pending > 0 {
				gs.Withdrawals = append(gs.Withdrawals, fibretypes.Withdrawal{Signer: signer, Amount: utia(pending), RequestedTimestamp: blockTime, AvailableTimestamp: blockTime})
			}
		})
	}
	// IBC: an open and a closed transfer channel.
	escrowAddr := ibctransfertypes.GetEscrowAddress(ibctransfertypes.PortID, "channel-0")
	transferChannels := chain(editModule(f, ibcexported.ModuleName, func(gs *ibccoretypes.GenesisState) {
		for i, state := range []channeltypes.State{channeltypes.OPEN, channeltypes.CLOSED} {
			gs.ChannelGenesis.Channels = append(gs.ChannelGenesis.Channels, channeltypes.IdentifiedChannel{PortId: ibctransfertypes.PortID, ChannelId: fmt.Sprintf("channel-%d", i), State: state})
		}
	}), editModule(f, ibctransfertypes.ModuleName, func(gs *ibctransfertypes.GenesisState) { gs.TotalEscrowed = sdk.NewCoins(utia(10)) }))
	token := func(id uint64, typ warptypes.HypTokenType, denom string, collateral int64) warptypes.HypToken {
		return warptypes.HypToken{Id: hyputil.GenerateHexAddress([20]byte{1}, 1, id), TokenType: typ, OriginDenom: denom, CollateralBalance: math.NewInt(collateral)}
	}
	tokens := func(ts ...warptypes.HypToken) edit {
		return editModule(f, warptypes.ModuleName, func(gs *warptypes.GenesisState) { gs.Tokens = append(gs.Tokens, ts...) })
	}
	routers := func(ids ...uint64) edit {
		return editModule(f, warptypes.ModuleName, func(gs *warptypes.GenesisState) {
			for _, id := range ids {
				gs.RemoteRouters = append(gs.RemoteRouters, warptypes.GenesisRemoteRouterWrapper{TokenId: id, RemoteRouter: warptypes.RemoteRouter{ReceiverDomain: 7, Gas: math.ZeroInt()}})
			}
		})
	}
	collateral, synthetic := warptypes.HYP_TOKEN_TYPE_COLLATERAL, warptypes.HYP_TOKEN_TYPE_SYNTHETIC

	testCases := []struct {
		name      string
		edit      edit
		ack       string              // AcceptMintExcess; empty means none
		blockTime time.Time           // ExportBlockTime; zero keeps the fixture's
		want      *genesis.CheckError // nil means accepted
		excess    string              // the MintExcess of an accepted case; empty means not checked
		check     func(t *testing.T, res *genesis.CheckResult)
	}{
		{name: "fixture accepted", check: func(t *testing.T, res *genesis.CheckResult) {
			require.Equal(t, bank.Supply.String(), res.Supply.String())
			require.Equal(t, bonded.String()+"utia", res.Balances[stakingtypes.BondedPoolName].String())
			require.Equal(t, "map[fibre: gov: transfer:]", fmt.Sprint(res.Surplus))
		}},
		{name: "non-empty gen_txs", edit: replaceEdit(genutiltypes.ModuleName, `"gen_txs":[]`, `"gen_txs":[{}]`), want: rec("genutil", "gen_txs", "empty")},
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
		{name: "unbonding entry counted", edit: chain(addUBD(acc), credit(stakingtypes.NotBondedPoolName, utia(5)))},
		{name: "upper-case alias of an unbonding delegation", edit: addUBD(acc, strings.ToUpper(acc)), want: rec("staking", fmt.Sprintf("unbonding_delegations[%d]", nUBDs+1), "a distinct key")},
		// delegations
		{name: "upper-case alias of a delegation", edit: stakingEdit(func(gs *stakingtypes.GenesisState) {
			d := gs.Delegations[0]
			d.DelegatorAddress = strings.ToUpper(d.DelegatorAddress)
			gs.Delegations = append(gs.Delegations, d)
		}), want: rec("staking", fmt.Sprintf("delegations[%d]", len(staking.Delegations)), "a distinct key")},
		{name: "zero delegation shares", edit: stakingEdit(func(gs *stakingtypes.GenesisState) { gs.Delegations[0].Shares = math.LegacyZeroDec() }), want: rec("staking", "delegations[0].shares", "> 0")},
		{name: "nil delegation shares", edit: rawEdit(stakingtypes.ModuleName, `,"shares":"`+staking.Delegations[0].Shares.String()+`"`), want: rec("staking", "delegations[0].shares", "> 0")},
		{name: "negative delegation shares", edit: stakingEdit(func(gs *stakingtypes.GenesisState) { gs.Delegations[0].Shares = math.LegacyNewDec(-1) }), want: rec("staking", "delegations[0].shares", "> 0")},
		{name: "invalid delegator address", edit: stakingEdit(func(gs *stakingtypes.GenesisState) { gs.Delegations[0].DelegatorAddress = "celestia1invalid" }), want: rec("staking", "delegations[0].delegator_address", "a valid address")},
		{name: "invalid delegation validator address", edit: stakingEdit(func(gs *stakingtypes.GenesisState) { gs.Delegations[0].ValidatorAddress = "celestiavaloper1invalid" }), want: rec("staking", "delegations[0].validator_address", "a valid address")},
		{name: "nil delegator shares", edit: rawEdit(stakingtypes.ModuleName, `,"delegator_shares":"`+staking.Validators[0].DelegatorShares.String()+`"`), want: cons("staking", "validators[0].delegator_shares", staking.Delegations[0].Shares.String())},
		{name: "delegation shares sum beyond the dec range", edit: stakingEdit(func(gs *stakingtypes.GenesisState) {
			d := &gs.Delegations[0]
			d.Shares = math.LegacyNewDecFromInt(maxInt)
			gs.Delegations = append(gs.Delegations, stakingtypes.NewDelegation(sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(), d.ValidatorAddress, d.Shares))
		}), want: cons("staking", "validators[0].delegator_shares", math.LegacyNewDecFromBigInt(new(big.Int).Lsh(maxInt.BigInt(), 1)).String())},
		{name: "delegation to an unknown validator", edit: stakingEdit(func(gs *stakingtypes.GenesisState) {
			gs.Delegations = append(gs.Delegations, stakingtypes.NewDelegation(acc, val1, math.LegacyOneDec()))
		}), want: rec("staking", fmt.Sprintf("delegations[%d].validator_address", len(staking.Delegations)), "a validator in validators")},
		{name: "delegator shares off by 1e-18", edit: stakingEdit(func(gs *stakingtypes.GenesisState) {
			gs.Validators[0].DelegatorShares = gs.Validators[0].DelegatorShares.Add(math.LegacySmallestDec())
		}), want: cons("staking", "validators[0].delegator_shares", staking.Validators[0].DelegatorShares.String())},
		{name: "delegations of a validator summed exactly", edit: stakingEdit(func(gs *stakingtypes.GenesisState) {
			d := &gs.Delegations[0]
			d.Shares = d.Shares.Sub(math.LegacySmallestDec())
			gs.Delegations = append(gs.Delegations, stakingtypes.NewDelegation(sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(), d.ValidatorAddress, math.LegacySmallestDec()))
		})},
		{name: "validator without delegations and zero shares", edit: stakingEdit(func(gs *stakingtypes.GenesisState) {
			v := gs.Validators[0]
			v.OperatorAddress, v.Status, v.Tokens, v.DelegatorShares = val1, stakingtypes.Unbonding, math.ZeroInt(), math.LegacyZeroDec()
			gs.Validators = append(gs.Validators, v)
		})},
		{name: "validator without delegations and non-zero shares", edit: stakingEdit(func(gs *stakingtypes.GenesisState) {
			v := gs.Validators[0]
			v.OperatorAddress, v.Status, v.Tokens, v.DelegatorShares = val1, stakingtypes.Unbonding, math.ZeroInt(), math.LegacySmallestDec()
			gs.Validators = append(gs.Validators, v)
		}), want: cons("staking", fmt.Sprintf("validators[%d].delegator_shares", nVals), "0.000000000000000000")},
		// distribution
		{name: "distribution off by one after truncation", edit: chain(distrEdit(func(gs *distrtypes.GenesisState) {
			gs.FeePool.CommunityPool = gs.FeePool.CommunityPool.Add(foo("1.5")...)
		}), credit(distrtypes.ModuleName, sdk.NewInt64Coin("foo", 2))), want: cons("distribution", "balance[foo]", "1")},
		{name: "distribution dust accepted", edit: distrEdit(func(gs *distrtypes.GenesisState) {
			gs.FeePool.CommunityPool = gs.FeePool.CommunityPool.Add(foo("0.5")...)
		})},
		{name: "distribution truncates after aggregation", edit: chain(addOutstanding("0.6", val1, val2), credit(distrtypes.ModuleName, sdk.NewInt64Coin("foo", 1)))},
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
		{name: "mint rate of a year before T", blockTime: blockTime.Add(time.Duration(minttypes.NanosecondsPerYear)), want: cons("mint", "minter.inflation_rate", mint.Minter.CalculateInflationRate(sdk.Context{}.WithBlockTime(blockTime.Add(time.Duration(minttypes.NanosecondsPerYear))), *mint.GenesisTime).String()+" at export-block-time")},
		{name: "mint genesis time after T", blockTime: blockTime.Add(-time.Nanosecond), want: cons("mint", "genesis_time", "<= export-block-time "+formatTime(blockTime.Add(-time.Nanosecond)))},
		// gov
		{name: "gov records accepted with a surplus", edit: chain(proposal(&afterT), vote("1", acc), deposit(acc), credit(govtypes.ModuleName, utia(7))), check: func(t *testing.T, res *genesis.CheckResult) {
			require.Equal(t, "2utia", res.Surplus[govtypes.ModuleName].String())
		}},
		{name: "gov deposits above the balance", edit: deposit(acc), want: cons("gov", "balance[utia]", ">= 5")},
		{name: "upper-case alias of a deposit", edit: deposit(acc, strings.ToUpper(acc)), want: rec("gov", "deposits[1]", "a distinct key")},
		{name: "deposit from a blocked address", edit: deposit(authtypes.NewModuleAddress(distrtypes.ModuleName).String()), want: rec("gov", "deposits[0].depositor", "not a blocked address")},
		{name: "null gov params", edit: govEdit(func(gs *govv1.GenesisState) { gs.Params = nil }), want: rec("gov", "params", "set")},
		{name: "null gov proposal", edit: replaceEdit(govtypes.ModuleName, `"proposals":[]`, `"proposals":[null]`), want: rec("gov", "proposals[0]", "set")},
		{name: "null proposal message", edit: chain(proposal(&afterT), replaceEdit(govtypes.ModuleName, `"messages":[]`, `"messages":[null]`)), want: rec("gov", "proposals[0].messages[0]", "set")},
		{name: "proposal message that panics on unpacking", edit: chain(proposal(&afterT), replaceEdit(govtypes.ModuleName, `"messages":[]`, `"messages":[{"@type":"/ibc.core.client.v1.MsgCreateClient","client_state":{"@type":"/ibc.lightclients.solomachine.v3.ClientState"}}]`)), want: rec("gov", "", "decodable records")},
		{name: "IBC client state that panics on unpacking", edit: replaceEdit(ibcexported.ModuleName, `"clients":[]`, `"clients":[{"client_id":"06-solomachine-0","client_state":{"@type":"/ibc.lightclients.solomachine.v3.ClientState","sequence":"1"}}]`), want: rec("ibc", "", "decodable records")},
		{name: "gov min deposit without an amount", edit: replaceEdit(govtypes.ModuleName, `"min_deposit":[`, `"min_deposit":[{"denom":"a"},`), want: rec("gov", "params.min_deposit[0].amount", "set")},
		{name: "null vote option", edit: chain(vote("1", acc), replaceEdit(govtypes.ModuleName, `"options":[`, `"options":[null,`)), want: rec("gov", "votes[0].options[0]", "set")},
		{name: "voting proposal without an end time", edit: proposal(nil), want: rec("gov", "proposals[0].voting_end_time", "set")},
		{name: "expedited voting proposal without a start time", edit: chain(proposal(&afterT), govEdit(func(gs *govv1.GenesisState) { gs.Proposals[0].Expedited = true })), want: rec("gov", "proposals[0].voting_start_time", "set")},
		{name: "expedited voting proposal with a start time and a deposit accepted", edit: chain(proposal(&afterT), govEdit(func(gs *govv1.GenesisState) {
			gs.Proposals[0].Expedited, gs.Proposals[0].VotingStartTime, gs.Proposals[0].TotalDeposit = true, &blockTime, sdk.NewCoins(utia(5))
		}))},
		{name: "negative total deposit", edit: govEdit(func(gs *govv1.GenesisState) {
			gs.Proposals = append(gs.Proposals, &govv1.Proposal{Id: 1, Status: govv1.StatusDepositPeriod, SubmitTime: &blockTime, DepositEndTime: &afterT, TotalDeposit: sdk.Coins{{Denom: "utia", Amount: math.NewInt(-1)}}})
			gs.StartingProposalId = 2
		}), want: rec("gov", "proposals[0].total_deposit", "valid positive coins")},
		{name: "gov submit time after T", edit: govEdit(func(gs *govv1.GenesisState) {
			gs.Proposals = append(gs.Proposals, &govv1.Proposal{Id: 1, SubmitTime: &afterT})
		}), want: cons("gov", "proposals[0].submit_time", notAfterT)},
		{name: "starting proposal id not above the largest", edit: chain(proposal(&afterT), govEdit(func(gs *govv1.GenesisState) { gs.StartingProposalId = 1 })), want: rec("gov", "starting_proposal_id", "> 1 (the largest proposal id)")},
		{name: "vote on a missing proposal", edit: vote("1", acc), want: rec("gov", "votes[0].proposal_id", "an existing proposal")},
		{name: "vote weights below 1", edit: vote("0.5", acc), want: rec("gov", "votes[0].options", "distinct valid options with weights summing to 1")},
		{name: "upper-case alias of a vote", edit: vote("1", acc, strings.ToUpper(acc)), want: rec("gov", "votes[1]", "a distinct key")},
		// fibre
		{name: "fibre balance below the escrows", edit: escrow(acc, 10, 10, 0), want: cons("fibre", "balance[utia]", ">= 10")},
		{name: "fibre escrow above available plus pending", edit: chain(escrow(acc, 10, 5, 0), credit(fibretypes.ModuleName, utia(10))), want: cons("fibre", "escrow_accounts["+acc+"].balance[utia]", "5 (available + pending)")},
		{name: "fibre case variants aggregated with a surplus", edit: chain(escrow(acc, 10, 0, 0), escrow(strings.ToUpper(acc), 10, 10, 10), credit(fibretypes.ModuleName, utia(21))), check: func(t *testing.T, res *genesis.CheckResult) {
			require.Equal(t, "[{"+acc+" utia 20 10 10}]", fmt.Sprint(res.FibreEscrows))
			require.Equal(t, "1utia", res.Surplus[fibretypes.ModuleName].String())
		}},
		{name: "fibre zero escrow listed", edit: escrow(acc, 0, 0, 0), check: func(t *testing.T, res *genesis.CheckResult) {
			require.Equal(t, "[{"+acc+" utia 0 0 0}]", fmt.Sprint(res.FibreEscrows))
		}},
		{name: "fibre escrow not in utia", edit: chain(escrow(acc, 1, 1, 0), replaceEdit(fibretypes.ModuleName, `"utia"`, `"foo"`), replaceEdit(fibretypes.ModuleName, `"utia"`, `"foo"`)), want: rec("fibre", "escrow_accounts[0].balance", "utia")},
		{name: "fibre withdrawal without an escrow", edit: fibreEdit(func(gs *fibretypes.GenesisState) {
			gs.Withdrawals = append(gs.Withdrawals, fibretypes.Withdrawal{Signer: acc, Amount: utia(1), RequestedTimestamp: blockTime, AvailableTimestamp: blockTime})
		}), want: rec("fibre", "", "valid records")},
		{name: "fibre withdrawal requested after T", edit: chain(escrow(acc, 1, 0, 0), fibreEdit(func(gs *fibretypes.GenesisState) {
			gs.Withdrawals = append(gs.Withdrawals, fibretypes.Withdrawal{Signer: acc, Amount: utia(1), RequestedTimestamp: afterT, AvailableTimestamp: afterT})
		})), want: cons("fibre", "withdrawals[0].requested_timestamp", notAfterT)},
		{name: "fibre payment processed after T", edit: fibreEdit(func(gs *fibretypes.GenesisState) {
			gs.ProcessedPayments = append(gs.ProcessedPayments, fibretypes.ProcessedPayment{PaymentPromiseHash: []byte{1}, ProcessedAt: afterT})
		}), want: cons("fibre", "processed_payments[0].processed_at", notAfterT)},
		{name: "fibre freshness floor after T", edit: fibreEdit(func(gs *fibretypes.GenesisState) { gs.PromiseFreshnessFloor = afterT }), want: cons("fibre", "promise_freshness_floor", notAfterT)},
		// IBC
		{name: "IBC escrow below total_escrowed", edit: chain(transferChannels, fund(escrowAddr, utia(9))), want: cons("transfer", "escrow_balances[utia]", ">= 10")},
		{name: "IBC escrows listed with a surplus", edit: chain(transferChannels, fund(escrowAddr, utia(11))), check: func(t *testing.T, res *genesis.CheckResult) {
			require.Equal(t, "[{transfer channel-0 11utia} {transfer channel-1 }]", fmt.Sprint(res.TransferEscrows))
			require.Equal(t, "1utia", res.Surplus[ibctransfertypes.ModuleName].String())
			require.Equal(t, "10utia", res.TotalEscrowed.String())
		}},
		{name: "IBC channel params with a zero upgrade timeout", edit: editModule(f, ibcexported.ModuleName, func(gs *ibccoretypes.GenesisState) { gs.ChannelGenesis.Params.UpgradeTimeout.Timestamp = 0 }), want: rec("ibc", "channel_genesis.params", "valid params")},
		{name: "IBC transfer port not the default", edit: editModule(f, ibctransfertypes.ModuleName, func(gs *ibctransfertypes.GenesisState) { gs.PortId = "other" }), want: rec("transfer", "port_id", "transfer")},
		// warp
		{name: "warp tokens listed and collateral aggregated", edit: chain(tokens(token(3, synthetic, "hyperlane/x", 5), token(1, collateral, "utia", 3), token(2, collateral, "utia", 4)), routers(1), credit(warptypes.ModuleName, utia(7))), check: func(t *testing.T, res *genesis.CheckResult) {
			require.Len(t, res.WarpTokens, 3)
			require.Equal(t, "3", res.WarpTokens[0].CollateralBalance.String())
			require.Equal(t, "5", res.WarpTokens[2].CollateralBalance.String())
			require.Len(t, res.WarpRouters, 1)
		}},
		{name: "duplicate warp token id", edit: tokens(token(1, collateral, "utia", 0), token(1, synthetic, "hyperlane/x", 0)), want: rec("warp", "tokens[1]", "a distinct key")},
		{name: "nil warp collateral", edit: chain(tokens(token(1, collateral, "utia", 7)), rawEdit(warptypes.ModuleName, `,"collateral_balance":"7"`)), want: rec("warp", "tokens[0].collateral_balance", ">= 0")},
		{name: "negative warp collateral", edit: tokens(token(1, collateral, "utia", -1)), want: rec("warp", "tokens[0].collateral_balance", ">= 0")},
		{name: "unspecified warp token type", edit: tokens(token(1, warptypes.HYP_TOKEN_TYPE_UNSPECIFIED, "utia", 0)), want: rec("warp", "tokens[0].token_type", "collateral or synthetic")},
		{name: "invalid denom of a synthetic token", edit: tokens(token(1, synthetic, "!", 0)), want: rec("warp", "tokens[0].origin_denom", "a valid denom")},
		{name: "unexpected denom in the warp balance", edit: credit(warptypes.ModuleName, sdk.NewInt64Coin("foo", 1)), want: cons("warp", "balance[foo]", "0")},
		{name: "router of a missing token", edit: routers(1), want: rec("warp", "remote_routers[0].token_id", "an existing token")},
		{name: "duplicate router", edit: chain(tokens(token(1, collateral, "utia", 0)), routers(1, 1)), want: rec("warp", "remote_routers[1]", "a distinct key")},
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
			if !tc.blockTime.IsZero() {
				opts.ExportBlockTime = tc.blockTime
			}
			opts.BlockedAddresses = map[string]bool{authtypes.NewModuleAddress(distrtypes.ModuleName).String(): true}

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
			if tc.check != nil {
				tc.check(t, res)
			}
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
	return replaceEdit(name, cut, "")
}

// replaceEdit returns an edit that replaces the first old in the named module's JSON with repl.
func replaceEdit(name, old, repl string) edit {
	return func(t *testing.T, g *genutiltypes.AppGenesis) {
		var state map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(g.AppState, &state))
		require.Contains(t, string(state[name]), old)
		setModule(t, g, name, []byte(strings.Replace(string(state[name]), old, repl, 1)))
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

func chain(edits ...edit) edit {
	return func(t *testing.T, g *genutiltypes.AppGenesis) {
		for _, e := range edits {
			e(t, g)
		}
	}
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
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
