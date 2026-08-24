package hardspoon_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/math"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/celestiaorg/celestia-app/v9/app/hardspoon"
	minfeetypes "github.com/celestiaorg/celestia-app/v9/x/minfee/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	"github.com/stretchr/testify/require"
)

func TestTransform(t *testing.T) {
	capp := testApp(t)
	f := newFixture()

	result, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
	require.NoError(t, err)

	t.Run("genesis doc", func(t *testing.T) {
		require.Equal(t, "mocha-5", result.Genesis.ChainID)
		require.Equal(t, int64(1), result.Genesis.InitialHeight)
		require.Equal(t, defaultOptions().GenesisTime, result.Genesis.GenesisTime)
		require.Empty(t, result.Genesis.Validators, "the validator set comes from gentxs")
		/*
				   celestia-appd genesis gentx rachid 10000000000utia \
				       --chain-id corto-1 \
				       --commission-rate 0.2 \
				       --commission-max-rate 0.5 \
				       --commission-max-change-rate 0.01 \
				       --min-self-delegation 1 \
				       --fees 1utia \
				       --moniker hehehe
			python3 -c '
			  import json,sys
			  json.dump(json.load(open(sys.argv[1])), open(sys.argv[2],"w"), separators=(",",":"))' ~/.celestia-app/config/genesis.json genesis-final.json
				celestia-appd hardspoon corto-1 ./arabica.json arabica_genesis.json --genesis-time "2026-07-01T14:00:00Z" --app-version 9 --report report_arabica.json
		*/
		// The export reports app version 0. Starting mocha-5 on that would run
		// the wrong state machine, so hardspoon overwrites it.
		require.Equal(t, uint64(9), result.Genesis.ConsensusParams.Version.App)
		// The 32 MiB block size is carried, not defaulted.
		require.Equal(t, int64(33_554_432), result.Genesis.ConsensusParams.Block.MaxBytes)
	})

	t.Run("stake is returned to its owners", func(t *testing.T) {
		require.Equal(t, math.NewInt(30_000).String(), result.Report.Credited.Principal.String())
		require.Equal(t, math.NewInt(300).String(), result.Report.Credited.Unbonding.String())
		require.Equal(t, math.NewInt(30_300).String(), result.Report.Credited.Pools.String())
		require.True(t, result.Report.Credited.Dust.IsZero(), "dust was %s", result.Report.Credited.Dust)

		balances := balancesByAddress(t, capp, result)
		// 1000 held + 10000 principal + 300 unbonding.
		require.Equal(t, "11300utia", balances[f.Delegator1].String())
		// Had no balance at all; its whole holding is returned stake.
		require.Equal(t, "20000utia", balances[f.Delegator2].String())
	})

	t.Run("unreachable and module balances are dropped", func(t *testing.T) {
		balances := balancesByAddress(t, capp, result)
		require.NotContains(t, balances, f.ICA, "interchain accounts have no key and no channel")
		require.NotContains(t, balances, f.Escrow, "escrow cannot be released once ibc is wiped")

		categories := map[string]sdk.Coins{}
		for _, entry := range result.Report.Vaporized {
			categories[entry.Category] = categories[entry.Category].Add(entry.Coins...)
		}
		require.Equal(t, "500utia", categories["interchain-account"].String())
		require.Equal(t, "700utia", categories["ibc-escrow"].String())
		require.Equal(t, "5000utia", categories["module:distribution"].String())
		require.Equal(t, "30000utia", categories["module:bonded_tokens_pool"].String())
		require.Equal(t, "300utia", categories["module:not_bonded_tokens_pool"].String())
		require.Equal(t, "7utia", categories["module:fee_collector"].String())

		require.Equal(t, 1, result.Report.EscrowChannels, "only the transfer-port channel counts")
		require.Equal(t, "700utia", result.Report.EscrowHeld.String())
	})

	t.Run("supply is recomputed from surviving balances", func(t *testing.T) {
		var bank banktypes.GenesisState
		appStateOf(t, capp.AppCodec(), result, banktypes.ModuleName, &bank)

		sum := sdk.NewCoins()
		for _, balance := range bank.Balances {
			sum = sum.Add(balance.Coins...)
		}
		require.Equal(t, sum.String(), bank.Supply.String())
		// 11300 + 20000 + 300 orphan.
		require.Equal(t, "31600utia", bank.Supply.String())
		// Carried, not defaulted.
		require.Len(t, bank.DenomMetadata, 1)
	})

	t.Run("accounts", func(t *testing.T) {
		accounts := accountsByAddress(t, capp, result)

		require.Contains(t, accounts, f.Delegator1)
		require.Contains(t, accounts, f.Delegator2)
		require.Contains(t, accounts, f.Vesting, "vesting accounts are never pruned")
		require.Contains(t, accounts, f.Orphan, "a balance with no account gets one")
		require.NotContains(t, accounts, f.Empty, "an account holding nothing is pruned")
		require.NotContains(t, accounts, f.ICA)
		require.NotContains(t, accounts, f.Escrow)
		require.Len(t, accounts, 4)

		require.Equal(t, 1, result.Report.Pruned)
		require.Equal(t, []string{f.Orphan}, result.Report.Adopted)

		for address, account := range accounts {
			require.Zero(t, account.GetSequence(), "sequence not reset for %s", address)
			require.Nil(t, account.GetPubKey(), "public key not dropped for %s", address)
		}
		// delegator1 was the only account carrying a public key.
		require.Equal(t, 1, result.Report.PubKeysStripped)
	})

	t.Run("vesting delegation tracking is cleared", func(t *testing.T) {
		accounts := accountsByAddress(t, capp, result)
		vesting, ok := accounts[f.Vesting].(*vestingtypes.ContinuousVestingAccount)
		require.True(t, ok)

		// Left set, these would understate the locked amount and let vesting
		// funds be spent early now that nothing is delegated.
		require.True(t, vesting.DelegatedVesting.IsZero())
		require.True(t, vesting.DelegatedFree.IsZero())
		require.Equal(t, "5000utia", vesting.OriginalVesting.String(), "the schedule itself is kept")

		require.Equal(t, 1, result.Report.VestingAccounts)
		require.Equal(t, 1, result.Report.VestingZeroed)
	})

	t.Run("history is dropped but parameters are carried", func(t *testing.T) {
		var staking stakingtypes.GenesisState
		appStateOf(t, capp.AppCodec(), result, stakingtypes.ModuleName, &staking)
		require.Empty(t, staking.Delegations)
		require.Empty(t, staking.UnbondingDelegations)
		require.True(t, staking.LastTotalPower.IsZero())
		require.False(t, staking.Exported)
		require.Equal(t, f.Staking.Params, staking.Params)

		var distribution distributiontypes.GenesisState
		appStateOf(t, capp.AppCodec(), result, distributiontypes.ModuleName, &distribution)
		require.Empty(t, distribution.DelegatorStartingInfos)
		require.True(t, distribution.FeePool.CommunityPool.IsZero(), "the community pool is not carried")
		require.Equal(t, f.Distribution.Params, distribution.Params)

		var gov govv1.GenesisState
		appStateOf(t, capp.AppCodec(), result, govtypes.ModuleName, &gov)
		require.Empty(t, gov.Proposals)
		require.Equal(t, uint64(1), gov.StartingProposalId)
		require.Equal(t, f.Gov.Params, gov.Params)
	})

	t.Run("minfee params are backfilled from the exported value", func(t *testing.T) {
		var minfee minfeetypes.GenesisState
		appStateOf(t, capp.AppCodec(), result, minfeetypes.ModuleName, &minfee)

		// The export only fills the deprecated top-level field while
		// InitGenesis reads only params, so without the backfill the new chain
		// would silently run on the default gas price.
		require.Equal(t, f.MinFee.NetworkMinGasPrice.String(), minfee.Params.NetworkMinGasPrice.String())
		require.Equal(t, f.MinFee.NetworkMinGasPrice.String(), minfee.NetworkMinGasPrice.String())
		require.NotEqual(t, minfeetypes.DefaultNetworkMinGasPrice.String(), minfee.Params.NetworkMinGasPrice.String())
	})

	t.Run("output is compact", func(t *testing.T) {
		require.NotContains(t, string(result.Bytes), "\n  ")
		require.Equal(t, len(result.Bytes), result.Report.SizeBytes)
	})
}

func TestTransformIsDeterministic(t *testing.T) {
	capp := testApp(t)

	first, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), newFixture().fork(t, capp), defaultOptions())
	require.NoError(t, err)
	second, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), newFixture().fork(t, capp), defaultOptions())
	require.NoError(t, err)

	// The published genesis is identified by its hash, so anyone re-running
	// hardspoon on the same export has to get the same bytes.
	require.Equal(t, string(first.Bytes), string(second.Bytes))
}

func TestRejectsPlainExport(t *testing.T) {
	capp := testApp(t)

	tests := map[string]struct {
		fixture func(*fixture)
		fork    func(*hardspoon.Fork)
		want    string
	}{
		"initial height is not zero": {
			fork: func(fork *hardspoon.Fork) { fork.InitialHeight = 13_125_871 },
			want: "initial_height 13125871",
		},
		"slash events survive": {
			fixture: func(f *fixture) {
				f.Distribution.ValidatorSlashEvents = []distributiontypes.ValidatorSlashEventRecord{{
					ValidatorAddress:    "celestiavaloper1abc",
					Height:              5_505_157,
					ValidatorSlashEvent: distributiontypes.ValidatorSlashEvent{ValidatorPeriod: 250, Fraction: math.LegacyNewDecWithPrec(1, 2)},
				}}
			},
			want: "no validator slash events",
		},
		"starting infos are not re-initialized": {
			fixture: func(f *fixture) {
				f.Distribution.DelegatorStartingInfos[0].StartingInfo.Height = 220_107
			},
			want: "starting infos re-initialized at height 0",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			if test.fixture != nil {
				test.fixture(f)
			}
			fork := f.fork(t, capp)
			if test.fork != nil {
				test.fork(fork)
			}

			_, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), fork, defaultOptions())
			require.Error(t, err)
			require.Contains(t, err.Error(), test.want)
			require.Contains(t, err.Error(), "--for-zero-height")
		})
	}
}

func TestPoolReconciliationCatchesOverCrediting(t *testing.T) {
	capp := testApp(t)
	f := newFixture()

	// Exactly what a plain export looks like: a stale starting stake, higher
	// than the delegation is now worth. On mocha-4 this affected 1,275 of 3,746
	// delegations and overshot the pools by 2.01M TIA.
	f.Distribution.DelegatorStartingInfos[0].StartingInfo.Stake = math.LegacyNewDec(11_000)

	_, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not reconcile with the staking module")
}

func TestPoolReconciliationToleratesTruncationDust(t *testing.T) {
	capp := testApp(t)
	f := newFixture()

	// Each delegation may lose up to one unit to truncation, so two delegations
	// may come up two units short. Anything more is a bug.
	f.Distribution.DelegatorStartingInfos[0].StartingInfo.Stake = math.LegacyNewDecWithPrec(99_999, 1) // 9999.9
	_, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
	require.NoError(t, err, "one unit of dust per delegation is expected")

	f.Distribution.DelegatorStartingInfos[1].StartingInfo.Stake = math.LegacyNewDec(19_990)
	_, err = hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
	require.Error(t, err, "10 units short is not truncation")
}

// TestPoolSurplusIsReportedNotFatal covers a pool holding more than its
// validators account for.
//
// mocha-4's bonded pool carries a round 1 TIA that no validator's tokens back,
// left over from a direct send to the pool address. It has nothing to do with
// crediting, it is written off with the rest of the module balance, and failing
// on it would block the regenesis over a pre-existing anomaly, so it is reported.
func TestPoolSurplusIsReportedNotFatal(t *testing.T) {
	capp := testApp(t)
	f := newFixture()

	surplus := int64(1_000_000)
	for i := range f.Balances {
		if f.Balances[i].Address == moduleAddress(stakingtypes.BondedPoolName) {
			f.Balances[i].Coins = sdk.NewCoins(sdk.NewInt64Coin(denom, 30_000+surplus))
		}
	}

	result, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
	require.NoError(t, err, "a pre-existing pool surplus must not block the spoon")

	// The crediting itself still reconciles exactly: the surplus is not dust.
	require.True(t, result.Report.Credited.Dust.IsZero(), "dust was %s", result.Report.Credited.Dust)
	require.Equal(t, math.NewInt(30_300).String(), result.Report.Credited.Staked.String())

	require.Len(t, result.Report.Credited.Surpluses, 1)
	reported := result.Report.Credited.Surpluses[0]
	require.Equal(t, stakingtypes.BondedPoolName, reported.Pool)
	require.Equal(t, math.NewInt(surplus).String(), reported.Surplus.String())
	require.Contains(t, result.Report.String(), "pool surplus")

	// And it leaves with the module balance rather than reaching anyone.
	var vaporized sdk.Coins
	for _, entry := range result.Report.Vaporized {
		if entry.Category == "module:"+stakingtypes.BondedPoolName {
			vaporized = entry.Coins
		}
	}
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(denom, 30_000+surplus)).String(), vaporized.String())
}

// TestPoolDeficitIsFatal is the other side of the surplus: a pool holding less
// than its validators account for means crediting would hand out tokens the pool
// never held, which is minting.
func TestPoolDeficitIsFatal(t *testing.T) {
	capp := testApp(t)
	f := newFixture()

	for i := range f.Balances {
		if f.Balances[i].Address == moduleAddress(stakingtypes.BondedPoolName) {
			f.Balances[i].Coins = sdk.NewCoins(sdk.NewInt64Coin(denom, 29_000))
		}
	}

	_, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
	require.Error(t, err)
	require.Contains(t, err.Error(), "tokens the pool never held")
}

func moduleAddress(name string) string {
	return authtypes.NewEmptyModuleAccount(name).GetAddress().String()
}

func TestEscrowGuardRefusesUnaccountedBalances(t *testing.T) {
	capp := testApp(t)

	t.Run("a missing channel is caught", func(t *testing.T) {
		f := newFixture()
		// Drop the transfer channel, so its escrow address is never derived and
		// its 700utia would be silently carried instead of dropped.
		f.Channels = f.Channels[1:]

		_, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
		require.Error(t, err)
		require.Contains(t, err.Error(), "do not match transfer.total_escrowed")
	})

	t.Run("a stray send to an escrow address is caught", func(t *testing.T) {
		f := newFixture()
		for i := range f.Balances {
			if f.Balances[i].Address == f.Escrow {
				f.Balances[i].Coins = sdk.NewCoins(sdk.NewInt64Coin(denom, 900))
			}
		}

		_, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
		require.Error(t, err)
		require.Contains(t, err.Error(), "do not match transfer.total_escrowed")
	})
}

// TestOrphanedWarpDenomsAreDropped covers the synthetic warp coins.
//
// The hyperlane, warp and zkism modules restart from the default genesis, so
// the routers that could redeem these coins on their origin chains do not
// survive the spoon, and any later Hyperlane deployment mints under fresh token
// ids. mocha-4 carries seven such denoms across nine accounts; carrying them
// would leave coins that are spendable but good for nothing.
func TestOrphanedWarpDenomsAreDropped(t *testing.T) {
	capp := testApp(t)
	f := newFixture()

	synthetic := f.addWarpToken(t, warptypes.HYP_TOKEN_TYPE_SYNTHETIC,
		"0x726f757465725f61707000000000000000000000000000020000000000000005")
	// A collateral token's coins are the chain's own denom, so registering one
	// must not put any utia at risk.
	collateral := f.addWarpToken(t, warptypes.HYP_TOKEN_TYPE_COLLATERAL,
		"0x726f757465725f61707000000000000000000000000000010000000000000000")
	require.Equal(t, denom, collateral)

	mixed := f.addAccount("warp-and-utia", sdk.NewCoins(
		sdk.NewInt64Coin(denom, 900), sdk.NewCoin(synthetic, math.NewInt(123)),
	))
	warpOnly := f.addAccount("warp-only", sdk.NewCoins(sdk.NewCoin(synthetic, math.NewInt(77))))

	result, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
	require.NoError(t, err)

	balances := balancesByAddress(t, capp, result)
	require.Equal(t, "900utia", balances[mixed].String(), "only the warp coins leave")
	require.NotContains(t, balances, warpOnly)
	require.NotContains(t, accountsByAddress(t, capp, result), warpOnly, "left holding nothing, so pruned")

	var bank banktypes.GenesisState
	appStateOf(t, capp.AppCodec(), result, banktypes.ModuleName, &bank)
	for _, coin := range bank.Supply {
		require.False(t, strings.HasPrefix(coin.Denom, "hyperlane/"), "supply still carries %s", coin)
	}

	require.Len(t, result.Report.OrphanedDenoms, 1)
	dropped := result.Report.OrphanedDenoms[0]
	require.Equal(t, synthetic, dropped.Denom)
	require.Equal(t, 2, dropped.Holders)
	require.Equal(t, "200", dropped.Amount.String())
	require.Contains(t, result.Report.String(), "orphaned denoms")
}

// TestOrphanedIBCVouchersAreDropped covers the transfer vouchers.
//
// The ibc module restarts from the default genesis, so every channel a voucher
// could be redeemed through is gone with it. mocha-4 carries exactly one, a
// transfer/channel-455/stake voucher under a single account; carrying it would
// leave the same orphan-asset problem the warp drop solves.
func TestOrphanedIBCVouchersAreDropped(t *testing.T) {
	capp := testApp(t)
	f := newFixture()

	trace := ibctransfertypes.DenomTrace{Path: "transfer/channel-455", BaseDenom: "stake"}
	f.Transfer.DenomTraces = append(f.Transfer.DenomTraces, trace)
	voucher := trace.IBCDenom()

	holder := f.addAccount("voucher-holder", sdk.NewCoins(
		sdk.NewInt64Coin(denom, 400), sdk.NewCoin(voucher, math.NewInt(1_000_000)),
	))
	// Metadata that names the voucher validly has to go with it; the invalid
	// entry ibc-go leaves behind is covered by TestInvalidDenomMetadataIsDropped.
	f.DenomMetadata = append(f.DenomMetadata, banktypes.Metadata{
		Base:    voucher,
		Display: "STAKE",
		Symbol:  "STAKE",
		Name:    "STAKE",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: voucher, Exponent: 0},
			{Denom: "STAKE", Exponent: 6},
		},
	})

	result, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
	require.NoError(t, err)

	balances := balancesByAddress(t, capp, result)
	require.Equal(t, "400utia", balances[holder].String(), "only the voucher leaves")

	var bank banktypes.GenesisState
	appStateOf(t, capp.AppCodec(), result, banktypes.ModuleName, &bank)
	for _, coin := range bank.Supply {
		require.False(t, strings.HasPrefix(coin.Denom, "ibc/"), "supply still carries %s", coin)
	}
	require.Len(t, bank.DenomMetadata, 1, "the voucher's metadata goes with it")
	require.Contains(t, result.Report.DenomMetadataDropped, voucher)

	var transfer ibctransfertypes.GenesisState
	appStateOf(t, capp.AppCodec(), result, ibctransfertypes.ModuleName, &transfer)
	require.Empty(t, transfer.DenomTraces, "a trace would name a voucher that no longer exists")
	require.Equal(t, f.Transfer.Params, transfer.Params, "parameters are still carried")

	require.Len(t, result.Report.OrphanedDenoms, 1)
	dropped := result.Report.OrphanedDenoms[0]
	require.Equal(t, voucher, dropped.Denom)
	require.Equal(t, 1, dropped.Holders)
	require.Equal(t, "1000000", dropped.Amount.String())
}

// TestUnregisteredIBCVoucherIsFatal pins down the guard for vouchers: an
// ibc/... coin that matches no denom trace the transfer module recorded means
// the derivation is wrong, and refusing beats silently deleting funds.
func TestUnregisteredIBCVoucherIsFatal(t *testing.T) {
	capp := testApp(t)
	f := newFixture()

	f.addAccount("stray-voucher", sdk.NewCoins(sdk.NewCoin(
		"ibc/0000000000000000000000000000000000000000000000000000000000000099",
		math.NewInt(5),
	)))

	_, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to drop a denom that cannot be accounted for")
}

// TestUnregisteredWarpDenomIsFatal pins down the guard: a hyperlane/... coin
// the exported warp genesis does not register as a synthetic token means the
// denom derivation is wrong, and refusing beats silently deleting funds.
func TestUnregisteredWarpDenomIsFatal(t *testing.T) {
	capp := testApp(t)
	f := newFixture()

	f.addAccount("stray-warp", sdk.NewCoins(sdk.NewCoin(
		"hyperlane/0x0000000000000000000000000000000000000000000000000000000000000099",
		math.NewInt(5),
	)))

	_, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to drop a denom that cannot be accounted for")
}

// TestVestingWarpDenomIsRefused covers a schedule denominated in a dropped
// coin. Removing the balance out from under original_vesting would corrupt the
// lock accounting, and no such account exists on mocha-4, so it is a refusal
// rather than a silent decision.
func TestVestingWarpDenomIsRefused(t *testing.T) {
	capp := testApp(t)
	f := newFixture()

	synthetic := f.addWarpToken(t, warptypes.HYP_TOKEN_TYPE_SYNTHETIC,
		"0x726f757465725f61707000000000000000000000000000020000000000000007")

	vestingAddress := address("warp-vesting")
	base := authtypes.NewBaseAccount(sdk.MustAccAddressFromBech32(vestingAddress), nil, uint64(len(f.Accounts)), 0)
	coins := sdk.NewCoins(sdk.NewCoin(synthetic, math.NewInt(1_000)))
	// Ends in 2033, so flattening never makes the question moot.
	account, err := vestingtypes.NewContinuousVestingAccount(base, coins, 1_600_000_000, 2_000_000_000)
	require.NoError(t, err)
	f.Accounts = append(f.Accounts, account)
	f.Balances = append(f.Balances, banktypes.Balance{Address: vestingAddress, Coins: coins})

	_, err = hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
	require.Error(t, err)
	require.Contains(t, err.Error(), "would corrupt its lock accounting")
}

// TestElapsedVestingSchedulesAreFlattened covers the vesting normalization.
//
// A schedule that ended before the new chain starts locks nothing, so a base
// account is the same account with less to go wrong. It also removes state a
// genesis file cannot legally hold: mocha-4 carries two ContinuousVestingAccounts
// whose start_time is after their end_time, which MsgCreateVestingAccount permits
// but ValidateGenesis rejects, so they would make the genesis unloadable.
func TestElapsedVestingSchedulesAreFlattened(t *testing.T) {
	capp := testApp(t)
	f := newFixture()
	opts := defaultOptions()
	at := opts.GenesisTime.Unix()

	elapsed := f.addVesting(t, "elapsed-vesting", at-1_000_000, at-1_000, 4_000)
	// start after end, exactly as two of mocha-4's accounts record it.
	malformed := f.addVesting(t, "malformed-vesting", at-1_000, at-2_000, 6_000)
	locked := f.addPermanentLocked(t, "permanently-locked", 7_000)

	result, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), opts)
	require.NoError(t, err)

	accounts := accountsByAddress(t, capp, result)
	require.IsType(t, &authtypes.BaseAccount{}, accounts[elapsed], "an elapsed schedule locks nothing")
	require.IsType(t, &authtypes.BaseAccount{}, accounts[malformed], "a malformed schedule cannot go in a genesis")
	require.IsType(t, &vestingtypes.PermanentLockedAccount{}, accounts[locked], "permanently locked never vests")
	// Still vesting at the genesis time, so left exactly as it was.
	require.IsType(t, &vestingtypes.ContinuousVestingAccount{}, accounts[f.Vesting])

	require.Equal(t, 2, result.Report.VestingFlattened)

	// Flattening must not move any tokens: the coins were spendable either way.
	balances := balancesByAddress(t, capp, result)
	require.Equal(t, "4000utia", balances[elapsed].String())
	require.Equal(t, "6000utia", balances[malformed].String())
	require.Equal(t, "7000utia", balances[locked].String())
}

// TestInvalidDenomMetadataIsDropped covers the entries ibc-go leaves behind.
//
// SetDenomMetadata writes the trace's base denom as the first denom unit while the
// metadata's base is the IBC hash, and Metadata.Validate requires the two to
// match, so every voucher a chain has received carries an entry that cannot go
// into a genesis file.
func TestInvalidDenomMetadataIsDropped(t *testing.T) {
	capp := testApp(t)
	f := newFixture()

	const voucher = "ibc/31EE285988C8524A953449AC2C17154270CB04C66C3115DE69827C36149BA2CF"
	f.DenomMetadata = append(f.DenomMetadata, banktypes.Metadata{
		Base:       voucher,
		Display:    "transfer/channel-455/stake",
		Symbol:     "STAKE",
		Name:       "transfer/channel-455/stake",
		DenomUnits: []*banktypes.DenomUnit{{Denom: "stake", Exponent: 0}},
	})

	result, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
	require.NoError(t, err)

	var bank banktypes.GenesisState
	appStateOf(t, capp.AppCodec(), result, banktypes.ModuleName, &bank)

	require.Len(t, bank.DenomMetadata, 1, "only the valid utia entry survives")
	require.Equal(t, denom, bank.DenomMetadata[0].Base)
	require.Equal(t, []string{voucher}, result.Report.DenomMetadataDropped)
	require.Contains(t, result.Report.String(), "denom metadata dropped")
}

// TestExpeditedVotingPeriodIsShortenedByOneSecond covers the carried gov params.
//
// arabica-11 runs with the expedited and regular voting periods both at 24h, which
// gov requires to be strictly ordered. One second is the entire adjustment on
// purpose: cutting the expedited period further would decide how fast governance
// may move on the new chain, which is not this tool's call.
func TestExpeditedVotingPeriodIsShortenedByOneSecond(t *testing.T) {
	capp := testApp(t)

	t.Run("equal periods are pushed apart by one second", func(t *testing.T) {
		f := newFixture()
		day := 24 * time.Hour
		f.Gov.Params.VotingPeriod = &day
		f.Gov.Params.ExpeditedVotingPeriod = &day

		result, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
		require.NoError(t, err)

		var gov govv1.GenesisState
		appStateOf(t, capp.AppCodec(), result, govtypes.ModuleName, &gov)
		require.Equal(t, day, *gov.Params.VotingPeriod, "the regular period is untouched")
		require.Equal(t, day-time.Second, *gov.Params.ExpeditedVotingPeriod)
		require.NoError(t, gov.Params.ValidateBasic())

		require.NotNil(t, result.Report.GovExpeditedVotingPeriod)
		require.Equal(t, day.String(), result.Report.GovExpeditedVotingPeriod.From)
		require.Equal(t, (day - time.Second).String(), result.Report.GovExpeditedVotingPeriod.To)
		require.Contains(t, result.Report.String(), "expedited voting period")
	})

	t.Run("an already ordered pair is carried verbatim", func(t *testing.T) {
		f := newFixture()
		voting, expedited := 48*time.Hour, 24*time.Hour
		f.Gov.Params.VotingPeriod = &voting
		f.Gov.Params.ExpeditedVotingPeriod = &expedited

		result, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
		require.NoError(t, err)

		var gov govv1.GenesisState
		appStateOf(t, capp.AppCodec(), result, govtypes.ModuleName, &gov)
		require.Equal(t, expedited, *gov.Params.ExpeditedVotingPeriod)
		require.Nil(t, result.Report.GovExpeditedVotingPeriod, "nothing to report when nothing moved")
	})

	t.Run("a voting period with no room below it is refused", func(t *testing.T) {
		f := newFixture()
		second := time.Second
		f.Gov.Params.VotingPeriod = &second
		f.Gov.Params.ExpeditedVotingPeriod = &second

		_, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), defaultOptions())
		require.Error(t, err)
		require.Contains(t, err.Error(), "no positive value below the regular period")
	})
}

// TestValidateGenesisHookRuns pins down that the hook is consulted and that its
// failure stops the spoon, since it is the only thing standing between state that
// is legal on a live chain but illegal in a genesis file and an opaque failure
// inside a later `genesis` subcommand.
func TestValidateGenesisHookRuns(t *testing.T) {
	capp := testApp(t)
	f := newFixture()

	var seen []string
	opts := defaultOptions()
	opts.ValidateGenesis = func(appState map[string]json.RawMessage) error {
		for module := range appState {
			seen = append(seen, module)
		}
		return fmt.Errorf("boom")
	}

	_, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "assembled genesis is not valid")
	require.Contains(t, err.Error(), "boom")
	require.Contains(t, seen, banktypes.ModuleName)
	require.Contains(t, seen, authtypes.ModuleName)
}

func TestKeepPubKeys(t *testing.T) {
	capp := testApp(t)
	f := newFixture()

	opts := defaultOptions()
	opts.KeepPubKeys = true

	result, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), opts)
	require.NoError(t, err)

	accounts := accountsByAddress(t, capp, result)
	require.NotNil(t, accounts[f.Delegator1].GetPubKey())
	require.Equal(t, 1, result.Report.PubKeysKept)
	require.Zero(t, result.Report.PubKeysStripped)
}

func TestNoPruneKeepsEmptyAccounts(t *testing.T) {
	capp := testApp(t)
	f := newFixture()

	opts := defaultOptions()
	opts.NoPrune = true

	result, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), opts)
	require.NoError(t, err)

	require.Contains(t, accountsByAddress(t, capp, result), f.Empty)
	require.Zero(t, result.Report.Pruned)
}

func TestReserves(t *testing.T) {
	capp := testApp(t)

	t.Run("are injected and counted", func(t *testing.T) {
		f := newFixture()
		opts := defaultOptions()
		reserve := address("company-reserve")
		opts.Reserves = []hardspoon.Reserve{{Address: reserve, Amount: "100000000000000utia"}}

		result, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), opts)
		require.NoError(t, err)

		require.Equal(t, "100000000000000utia", balancesByAddress(t, capp, result)[reserve].String())
		require.Equal(t, "100000000000000utia", result.Report.Reserves.String())
		require.Equal(t, "100000000031600utia", result.Report.Out.Supply.String())
	})

	t.Run("must not collide with a carried account", func(t *testing.T) {
		f := newFixture()
		opts := defaultOptions()
		opts.Reserves = []hardspoon.Reserve{{Address: f.Delegator1, Amount: "1utia"}}

		_, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), opts)
		require.Error(t, err)
		require.Contains(t, err.Error(), "already exists in the export")
	})

	t.Run("reject a malformed amount", func(t *testing.T) {
		f := newFixture()
		opts := defaultOptions()
		opts.Reserves = []hardspoon.Reserve{{Address: address("fresh"), Amount: "not-a-coin"}}

		_, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), opts)
		require.Error(t, err)
	})
}

func TestSizeCap(t *testing.T) {
	capp := testApp(t)
	f := newFixture()

	opts := defaultOptions()
	opts.MaxSizeBytes = 1024

	_, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "over the 1024 byte cap")
}

func TestOptionsAreValidated(t *testing.T) {
	capp := testApp(t)
	f := newFixture()
	fork := f.fork(t, capp)

	tests := map[string]func(*hardspoon.Options){
		"chain id is required":       func(o *hardspoon.Options) { o.ChainID = "" },
		"initial height must be >=1": func(o *hardspoon.Options) { o.InitialHeight = 0 },
		"app version is required":    func(o *hardspoon.Options) { o.AppVersion = 0 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			opts := defaultOptions()
			mutate(&opts)
			_, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), fork, opts)
			require.Error(t, err)
		})
	}
}

func balancesByAddress(t *testing.T, capp *app.App, result *hardspoon.Result) map[string]sdk.Coins {
	t.Helper()
	var bank banktypes.GenesisState
	appStateOf(t, capp.AppCodec(), result, banktypes.ModuleName, &bank)

	out := make(map[string]sdk.Coins, len(bank.Balances))
	for _, balance := range bank.Balances {
		out[balance.Address] = balance.Coins
	}
	return out
}

func accountsByAddress(t *testing.T, capp *app.App, result *hardspoon.Result) map[string]sdk.AccountI {
	t.Helper()
	var auth authtypes.GenesisState
	appStateOf(t, capp.AppCodec(), result, authtypes.ModuleName, &auth)

	accounts, err := authtypes.UnpackAccounts(auth.Accounts)
	require.NoError(t, err)

	out := make(map[string]sdk.AccountI, len(accounts))
	for _, account := range accounts {
		out[account.GetAddress().String()] = account
	}
	return out
}
