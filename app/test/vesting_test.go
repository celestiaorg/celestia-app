package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	tmtime "github.com/tendermint/tendermint/types/time"
	"google.golang.org/grpc"
)

const (
	totalAccountsPerType = 300
	initBalanceForGasFee = 10
)

type _accountDispenser struct {
	names   []string
	counter int
}

type _accountType int

const (
	RegularAccountType _accountType = iota + 1
	DelayedVestingAccountType
	PeriodicVestingAccountType
	ContinuousVestingAccountType
)

type VestingModuleTestSuite struct {
	suite.Suite

	accounts    sync.Map // map[_accountType]_accountDispenser
	accountsMut sync.Mutex

	kr   keyring.Keyring
	cctx testnode.Context
}

func TestVestingModule(t *testing.T) {
	suite.Run(t, new(VestingModuleTestSuite))
}

func (s *VestingModuleTestSuite) SetupSuite() {
	t := s.T()
	t.Log("setting up vesting module test suite")

	s.kr = testfactory.GenerateKeyring()

	genOpts := []testnode.GenesisOption{}
	genOpts = append(genOpts, s.initRegularAccounts(totalAccountsPerType))
	genOpts = append(genOpts, s.initDelayedVestingAccounts(totalAccountsPerType))
	genOpts = append(genOpts, s.initPeriodicVestingAccounts(totalAccountsPerType))
	genOpts = append(genOpts, s.initContinuousVestingAccounts(totalAccountsPerType))

	s.startNewNetworkWithGenesisOpt(genOpts...)
}

// NewNetworkWithGenesisOpt creates a new test network with the specified genesis options for the VestingModuleTestSuite.
// It initializes a default Tendermint configuration (tmCfg) and default application configuration (appConf).
// The target block time is set to 1 millisecond. It applies the given genesis options.
// The function returns the created client context (cctx).
// The keyring of the context is set to the keyring (s.kr) of the VestingModuleTestSuite.
//
// Parameters:
// - genesisOpts: The genesis options to be applied when creating the test network.
//
// Returns:
// The created client context (testnode.Context) for the new network.
func (s *VestingModuleTestSuite) startNewNetworkWithGenesisOpt(genesisOpts ...testnode.GenesisOption) {
	tmCfg := testnode.DefaultTendermintConfig()
	tmCfg.Consensus.TargetHeightDuration = time.Millisecond * 1
	appConf := testnode.DefaultAppConfig()

	cctx, _, _ := testnode.NewNetwork(s.T(), testnode.DefaultParams(), tmCfg, appConf, []string{}, genesisOpts...)
	s.cctx = cctx
	s.cctx.Keyring = s.kr
}

func (s *VestingModuleTestSuite) TestGenesisDelayedVestingAccountsSpendableBalance() {
	assert.NoError(s.T(), s.cctx.WaitForNextBlock())

	for {
		name := s.unusedAccount(DelayedVestingAccountType)
		address := getAddress(name, s.cctx.Keyring).String()

		resAccBytes, err := GetRawAccountInfo(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)

		var vAcc vestingtypes.DelayedVestingAccount
		err = vAcc.Unmarshal(resAccBytes)
		assert.NoError(s.T(), err)

		alreadyVested := vAcc.EndTime < tmtime.Now().Unix()

		balances, err := GetAccountSpendableBalance(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)
		expectedSpendableBal := initBalanceForGasFee
		if alreadyVested {
			expectedSpendableBal += testfactory.BaseAccountDefaultBalance
		}
		assert.EqualValues(s.T(),
			expectedSpendableBal,
			balances.AmountOf(app.BondDenom).Int64(),
			"spendable balance must match")

		// Continue testing until find an account with vesting (locked) balance
		// because we want to test both vested & vesting accounts
		if !alreadyVested {
			break
		}
	}
}

func (s *VestingModuleTestSuite) TestGenesisDelayedVestingAccountsTransfer() {
	assert.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account with endTime which is
	// 	at least 10 seconds away time from now
	for {
		name := s.unusedAccount(DelayedVestingAccountType)
		address := getAddress(name, s.cctx.Keyring).String()

		resAccBytes, err := GetRawAccountInfo(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)

		var vAcc vestingtypes.DelayedVestingAccount
		err = vAcc.Unmarshal(resAccBytes)
		assert.NoError(s.T(), err)

		if vAcc.EndTime > tmtime.Now().Unix()+10 {
			s.testTransferVestingAmount(name)
			return
		}
	}
}

func (s *VestingModuleTestSuite) TestGenesisDelayedVestingAccountsDelegation() {
	assert.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account that has some vesting (locked) balance
	for {
		name := s.unusedAccount(DelayedVestingAccountType)
		address := getAddress(name, s.cctx.Keyring).String()

		resAccBytes, err := GetRawAccountInfo(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)

		var vAcc vestingtypes.DelayedVestingAccount
		err = vAcc.Unmarshal(resAccBytes)
		assert.NoError(s.T(), err)

		// 10 seconds is chosen to be on the safe side
		if vAcc.EndTime > tmtime.Now().Unix()+10 {
			s.testDelegatingVestingAmount(name)
			return
		}
	}
}

func (s *VestingModuleTestSuite) TestGenesisDelayedVestingAccountsClaimDelegationRewards() {
	assert.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account with endTime which is
	// 	at least 20 seconds away time from now
	for {
		name := s.unusedAccount(DelayedVestingAccountType)
		address := getAddress(name, s.cctx.Keyring).String()

		resAccBytes, err := GetRawAccountInfo(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)

		var vAcc vestingtypes.DelayedVestingAccount
		err = vAcc.Unmarshal(resAccBytes)
		assert.NoError(s.T(), err)

		if vAcc.EndTime > tmtime.Now().Unix()+20 {
			s.testDelegatingVestingAmount(name)
			s.testClaimDelegationReward(name)
			return
		}
	}
}

func (s *VestingModuleTestSuite) TestGenesisPeriodicVestingAccountsSpendableBalance() {
	assert.NoError(s.T(), s.cctx.WaitForNextBlock())

	initCoinsForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(initBalanceForGasFee))

	for {
		name := s.unusedAccount(PeriodicVestingAccountType)
		address := getAddress(name, s.cctx.Keyring).String()

		resAccBytes, err := GetRawAccountInfo(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)

		var vAcc vestingtypes.PeriodicVestingAccount
		err = vAcc.Unmarshal(resAccBytes)
		assert.NoError(s.T(), err)

		balances, err := GetAccountSpendableBalance(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)

		vestedCoins := vAcc.GetVestedCoins(tmtime.Now())
		expectedSpendableCoins := vestedCoins.Add(initCoinsForGasFee)
		assert.EqualValues(s.T(),
			expectedSpendableCoins.AmountOf(app.BondDenom).Int64(),
			balances.AmountOf(app.BondDenom).Int64(),
			"spendable balance must match")

		// Stop testing once we hit an account with no spendable balance
		if vestedCoins.IsZero() {
			break
		}
		s.T().Log("waiting 3 seconds...")
		time.Sleep(3 * time.Second)
	}
}

func (s *VestingModuleTestSuite) TestGenesisPeriodicVestingAccountsDelegation() {
	assert.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account that has some vesting (locked) balance
	for {
		name := s.unusedAccount(PeriodicVestingAccountType)
		address := getAddress(name, s.cctx.Keyring).String()

		resAccBytes, err := GetRawAccountInfo(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)

		var vAcc vestingtypes.PeriodicVestingAccount
		err = vAcc.Unmarshal(resAccBytes)
		assert.NoError(s.T(), err)

		// 10 seconds is chosen to be on the safe side
		if vAcc.StartTime > tmtime.Now().Unix()+10 {
			s.testDelegatingVestingAmount(name)
			return
		}
	}
}

// This test function tests delegation of a periodic vesting account that
// has part of its allocation unlocked and part of it locked
func (s *VestingModuleTestSuite) TestGenesisPeriodicVestingAccountsDelegationPartiallyVested() {
	assert.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account that has some vesting (locked) and
	// some vested (unlocked) balance
	for {
		name := s.unusedAccount(PeriodicVestingAccountType)
		address := getAddress(name, s.cctx.Keyring).String()

		resAccBytes, err := GetRawAccountInfo(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)

		var vAcc vestingtypes.PeriodicVestingAccount
		err = vAcc.Unmarshal(resAccBytes)
		assert.NoError(s.T(), err)

		firstPeriodTime := vAcc.StartTime + vAcc.VestingPeriods[0].GetLength()
		if firstPeriodTime < tmtime.Now().Unix() {
			continue
		}

		// an account that has its first period passed by just a little
		// and so only a part of allocation is unlocked
		if firstPeriodTime >= tmtime.Now().Unix() {
			waitTime := firstPeriodTime - tmtime.Now().Unix() + 1
			s.T().Logf("waiting %d seconds...", waitTime)
			time.Sleep(time.Duration(waitTime) * time.Second)
		}
		assert.False(s.T(), vAcc.GetVestedCoins(tmtime.Now()).IsZero(), "unlocked balance amount must not be zero")

		s.testDelegatingVestingAmount(name)
		return
	}
}

func (s *VestingModuleTestSuite) TestGenesisPeriodicVestingAccountsClaimDelegationRewards() {
	assert.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account that has some vesting (locked) balance
	// 	to be on the safe side we select one that starts unlocking in at least 20 seconds
	for {
		name := s.unusedAccount(PeriodicVestingAccountType)
		address := getAddress(name, s.cctx.Keyring).String()

		resAccBytes, err := GetRawAccountInfo(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)

		var vAcc vestingtypes.PeriodicVestingAccount
		err = vAcc.Unmarshal(resAccBytes)
		assert.NoError(s.T(), err)

		if vAcc.StartTime > tmtime.Now().Unix()+20 {
			s.testDelegatingVestingAmount(name)
			s.testClaimDelegationReward(name)
			return
		}
	}
}

func (s *VestingModuleTestSuite) TestGenesisContinuousVestingAccountsSpendableBalance() {
	assert.NoError(s.T(), s.cctx.WaitForNextBlock())

	initCoinsForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(initBalanceForGasFee))

	for {
		name := s.unusedAccount(ContinuousVestingAccountType)
		address := getAddress(name, s.cctx.Keyring).String()

		resAccBytes, err := GetRawAccountInfo(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)

		var vAcc vestingtypes.ContinuousVestingAccount
		err = vAcc.Unmarshal(resAccBytes)
		assert.NoError(s.T(), err)

		balances, err := GetAccountSpendableBalance(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)

		vestedCoins := vAcc.GetVestedCoins(tmtime.Now())
		maxExpectedSpendableBalCoins := vestedCoins.Add(initCoinsForGasFee)
		assert.LessOrEqual(s.T(),
			balances.AmountOf(app.BondDenom).Int64(),
			maxExpectedSpendableBalCoins.AmountOf(app.BondDenom).Int64())

		// Stop testing once we hit an account with no spendable balance
		if vestedCoins.IsZero() {
			break
		}
		s.T().Log("waiting 3 seconds...")
		time.Sleep(3 * time.Second)
	}
}

func (s *VestingModuleTestSuite) TestGenesisContinuousVestingAccountsDelegation() {
	assert.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account that has some vesting (locked) balance
	for {
		name := s.unusedAccount(ContinuousVestingAccountType)
		address := getAddress(name, s.cctx.Keyring).String()

		resAccBytes, err := GetRawAccountInfo(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)

		var vAcc vestingtypes.ContinuousVestingAccount
		err = vAcc.Unmarshal(resAccBytes)
		assert.NoError(s.T(), err)

		// 10 seconds is chosen to be on the safe side
		if vAcc.StartTime > tmtime.Now().Unix()+10 {
			s.testDelegatingVestingAmount(name)
			return
		}
	}
}

// This test function tests delegation of a continuous vesting account that
// has part of its allocation unlocked and part of it locked
func (s *VestingModuleTestSuite) TestGenesisContinuousVestingAccountsDelegationPartiallyVested() {
	assert.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account that has some vesting (locked) and
	// some vested (unlocked) balance
	for {
		name := s.unusedAccount(ContinuousVestingAccountType)
		address := getAddress(name, s.cctx.Keyring).String()

		resAccBytes, err := GetRawAccountInfo(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)

		var vAcc vestingtypes.ContinuousVestingAccount
		err = vAcc.Unmarshal(resAccBytes)
		assert.NoError(s.T(), err)

		if vAcc.StartTime < tmtime.Now().Unix() {
			continue
		}

		// an account that has its start time passed by just a little
		if vAcc.StartTime >= tmtime.Now().Unix() {
			waitTime := vAcc.StartTime - tmtime.Now().Unix() + 1
			s.T().Logf("waiting %d seconds...", waitTime)
			time.Sleep(time.Duration(waitTime) * time.Second)
		}
		assert.False(s.T(), vAcc.GetVestedCoins(tmtime.Now()).IsZero(), "unlocked balance amount must not be zero")

		s.testDelegatingVestingAmount(name)
		return
	}
}

func (s *VestingModuleTestSuite) TestGenesisContinuousVestingAccountsClaimDelegationRewards() {
	assert.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account that has some vesting (locked) balance
	// to be on the safe side we select one that starts unlocking in at least 20 seconds
	for {
		name := s.unusedAccount(ContinuousVestingAccountType)
		address := getAddress(name, s.cctx.Keyring).String()

		resAccBytes, err := GetRawAccountInfo(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)

		var vAcc vestingtypes.ContinuousVestingAccount
		err = vAcc.Unmarshal(resAccBytes)
		assert.NoError(s.T(), err)

		if vAcc.StartTime > tmtime.Now().Unix()+20 {
			s.testDelegatingVestingAmount(name)
			s.testClaimDelegationReward(name)
			return
		}
	}
}

// testTransferVestingAmount tests the transfer of vesting amounts (locked balance) from a vesting account
// to another account. It takes the name of the vesting account (name) as an input.
// It retrieves a random unused regular account and attempts to transfer the locked amount from the vesting
// account to the random account. It asserts that the result code of the transaction is equal to 5,
// indicating a failure in the transfer.
func (s *VestingModuleTestSuite) testTransferVestingAmount(name string) {
	randomAcc := s.unusedAccount(RegularAccountType)
	msgSend := banktypes.NewMsgSend(
		getAddress(name, s.cctx.Keyring),
		getAddress(randomAcc, s.cctx.Keyring),
		sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(testfactory.BaseAccountDefaultBalance))), // try to transfer the locked amount
	)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	resTx, err := testnode.SignAndBroadcastTx(encCfg, s.cctx.Context, name, []sdk.Msg{msgSend}...)
	assert.NoError(s.T(), err)

	resQ, err := s.cctx.WaitForTx(resTx.TxHash, 10)
	assert.NoError(s.T(), err)

	assert.EqualValues(s.T(), 5, resQ.TxResult.Code, "the transfer TX must fail")
}

// testDelegatingVestingAmount tests the delegation of vesting amounts (locked) from a vesting account to a validator.
// It takes the name of the vesting account (name) as an input and attempts to delegate the entire vesting amount to
// a validator. The delegation transaction should go through and then it retrieves the account delegations again for
// the given vesting account. It asserts that the delegated amount matches the locked amount.
func (s *VestingModuleTestSuite) testDelegatingVestingAmount(name string) {
	address := getAddress(name, s.cctx.Keyring).String()

	del, err := GetAccountDelegations(s.cctx.GRPCClient, address)
	assert.NoError(s.T(), err)
	assert.Empty(s.T(), del, "initial delegation must be empty")

	validators, err := GetValidators(s.cctx.GRPCClient)
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), validators)

	msgDelg := stakingtypes.NewMsgDelegate(
		getAddress(name, s.cctx.Keyring),
		validators[0].GetOperator(),
		sdk.NewCoin(app.BondDenom, sdk.NewInt(testfactory.BaseAccountDefaultBalance)),
	)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	resTx, err := testnode.SignAndBroadcastTx(encCfg, s.cctx.Context, name, []sdk.Msg{msgDelg}...)
	assert.NoError(s.T(), err)

	resQ, err := s.cctx.WaitForTx(resTx.TxHash, 10)
	assert.NoError(s.T(), err)
	assert.EqualValues(s.T(), 0, resQ.TxResult.Code, "the delegation TX must succeed")

	// verify the delegations
	del, err = GetAccountDelegations(s.cctx.GRPCClient, address)
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), del, "delegations must not be empty")
	assert.EqualValues(s.T(),
		testfactory.BaseAccountDefaultBalance,
		del[0].Balance.Amount.Int64(),
		"delegation amount must match")
}

// testClaimDelegationReward tests the claiming of delegation rewards for a vesting account.
// It takes the name of the vesting account (name) as an input.
// It claims the delegation rewards and then retrieves the balances of the vesting account.
// It asserts that the balance after claiming the reward.
func (s *VestingModuleTestSuite) testClaimDelegationReward(name string) {
	assert.NoError(s.T(), s.cctx.WaitForNextBlock())

	address := getAddress(name, s.cctx.Keyring).String()

	cli := distributiontypes.NewQueryClient(s.cctx.GRPCClient)
	resR, err := cli.DelegationTotalRewards(
		context.Background(),
		&distributiontypes.QueryDelegationTotalRewardsRequest{
			DelegatorAddress: address,
		},
	)
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), resR, "empty staking rewards")
	assert.NotEmpty(s.T(), resR.Rewards)

	rewardAmount := resR.Rewards[0].Reward.AmountOf(app.BondDenom).RoundInt().Int64()
	assert.Greater(s.T(), rewardAmount, int64(0), "rewards must be more than zero")

	balancesBefore, err := GetAccountSpendableBalance(s.cctx.GRPCClient, address)
	assert.NoError(s.T(), err)

	// minExpectedBalance is used because more tokens may be vested to the
	// account in the middle of this test
	minExpectedBalance := balancesBefore.AmountOf(app.BondDenom).Int64() + rewardAmount

	validators, err := GetValidators(s.cctx.GRPCClient)
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), validators, "empty validators set")

	msg := distributiontypes.NewMsgWithdrawDelegatorReward(
		getAddress(name, s.cctx.Keyring),
		validators[0].GetOperator(),
	)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	resTx, err := testnode.SignAndBroadcastTx(encCfg, s.cctx.Context, name, []sdk.Msg{msg}...)
	assert.NoError(s.T(), err)

	resQ, err := s.cctx.WaitForTx(resTx.TxHash, 10)
	assert.NoError(s.T(), err)
	assert.EqualValues(s.T(), 0, resQ.TxResult.Code, "the claim reward TX must succeed")

	// Check if the reward amount in the account
	balancesAfter, err := GetAccountSpendableBalance(s.cctx.GRPCClient, address)
	assert.NoError(s.T(), err)

	assert.GreaterOrEqual(s.T(), balancesAfter.AmountOf(app.BondDenom).Int64(), minExpectedBalance, "Minimum balance after claiming reward")
}

// initRegularAccounts initializes regular accounts for the VestingModuleTestSuite.
// It generates the specified number of account names and stores them in the accounts map
// of the VestingModuleTestSuite with RegularAccountType as the key. It also generates base accounts
// and their default balances. The generated accounts and balances are added to the provided genesis
// state. The genesis state modifier function is returned.
//
// Parameters:
// - count: The number of regular accounts to generate.
//
// Returns:
// A GenesisOption function that takes the current genesis state as input, adds the generated accounts and balances to it,
// and returns the modified genesis state.
func (s *VestingModuleTestSuite) initRegularAccounts(count int) testnode.GenesisOption {
	names := testfactory.GenerateAccounts(count)
	s.accounts.Store(RegularAccountType, _accountDispenser{names: names})
	bAccounts, balances := generateBaseAccounts(s.kr, names)

	return func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		gAccounts := authtypes.GenesisAccounts{}
		for i := range bAccounts {
			gAccounts = append(gAccounts, &bAccounts[i])
		}

		var err error
		gs, err = AddAccountsToGenesisState(gs, gAccounts...)
		assert.NoError(s.T(), err)

		gs, err = AddBalancesToGenesisState(gs, balances...)
		assert.NoError(s.T(), err)

		return gs
	}
}

// initDelayedVestingAccounts initializes delayed vesting accounts for the VestingModuleTestSuite.
// It generates the specified number of account names and stores them in the accounts map with
// RegularAccountType as the key. The generated accounts and balances are delayed vesting accounts
// with varying progressive end times which enables to have a number of vesting accounts (locked) and
// some vested accounts (unlocked balances) to test various scenarios. A modifier function which
// modifies genesis state, including the delayed vesting accounts and their balances, is returned
// as a GenesisOption function that takes the current genesis state as input.
//
// Parameters:
// - count: The number of delayed vesting accounts to generate.
//
// Returns:
// A GenesisOption function that takes the current genesis state as input, creates delayed vesting accounts
// with varying end times using the generated accounts and balances, adds them to the genesis state,
// and returns the modified genesis state.
func (s *VestingModuleTestSuite) initDelayedVestingAccounts(count int) testnode.GenesisOption {
	names := testfactory.GenerateAccounts(count)
	s.accounts.Store(DelayedVestingAccountType, _accountDispenser{names: names})
	bAccounts, balances := generateBaseAccounts(s.kr, names)

	initCoinsForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(initBalanceForGasFee))
	return func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		endTime := tmtime.Now().Add(-2 * time.Second)

		gAccounts := authtypes.GenesisAccounts{}
		for i := range bAccounts {
			bal := findBalanceByAddress(balances, bAccounts[i].GetAddress().String())
			assert.NotNil(s.T(), bal)
			coins := bal.Coins.Sub(initCoinsForGasFee)
			acc := vestingtypes.NewDelayedVestingAccount(&bAccounts[i], coins, endTime.Unix())
			gAccounts = append(gAccounts, acc)

			// the endTime is increased for each account to be able to test various scenarios
			endTime = endTime.Add(5 * time.Second)
		}

		var err error
		gs, err = AddAccountsToGenesisState(gs, gAccounts...)
		assert.NoError(s.T(), err)

		gs, err = AddBalancesToGenesisState(gs, balances...)
		assert.NoError(s.T(), err)

		return gs
	}
}

// initPeriodicVestingAccounts function initializes periodic vesting accounts for testing purposes.
// It takes the count of accounts as input and returns a GenesisOption. It generates account names,
// base accounts, and balances. It defines vesting periods and creates vesting accounts based on the
// generated data. The function then updates the genesis state with the generated vesting accounts
// and balances. The startTime of each account increases progressively to ensure some accounts have
// locked balances, catering to the testing requirements.
//
// Parameters:
// - count: The number of regular accounts to generate.
//
// Returns:
// A GenesisOption function that takes the current genesis state as input, adds the generated accounts
// and balances to it, and returns the modified genesis state.
func (s *VestingModuleTestSuite) initPeriodicVestingAccounts(count int) testnode.GenesisOption {
	names := testfactory.GenerateAccounts(count)
	s.accounts.Store(PeriodicVestingAccountType, _accountDispenser{names: names})
	bAccounts, balances := generateBaseAccounts(s.kr, names)

	allocationPerPeriod := int64(testfactory.BaseAccountDefaultBalance / 4)
	periods := vestingtypes.Periods{
		vestingtypes.Period{Length: int64(6), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, allocationPerPeriod)}},
		vestingtypes.Period{Length: int64(6), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, allocationPerPeriod)}},
		vestingtypes.Period{Length: int64(6), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, allocationPerPeriod)}},
		vestingtypes.Period{Length: int64(6), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, allocationPerPeriod)}},
	}

	initCoinsForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(initBalanceForGasFee))
	return func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		startTime := tmtime.Now()

		gAccounts := authtypes.GenesisAccounts{}
		for i := range bAccounts {
			bal := findBalanceByAddress(balances, bAccounts[i].GetAddress().String())
			assert.NotNil(s.T(), bal)
			coins := bal.Coins.Sub(initCoinsForGasFee)
			acc := vestingtypes.NewPeriodicVestingAccount(&bAccounts[i], coins, startTime.Unix(), periods)
			gAccounts = append(gAccounts, acc)

			// the startTime is increased for each account to be able to test various scenarios
			startTime = startTime.Add(5 * time.Second)
		}

		var err error
		gs, err = AddAccountsToGenesisState(gs, gAccounts...)
		assert.NoError(s.T(), err)

		gs, err = AddBalancesToGenesisState(gs, balances...)
		assert.NoError(s.T(), err)

		return gs
	}
}

// initContinuousVestingAccounts function initializes continuous vesting accounts for testing purposes.
// It takes the count of accounts as input and returns a GenesisOption. It generates account names,
// base accounts, and balances. It defines start & end times to creates vesting accounts. The function
// then updates the genesis state with the generated vesting accounts and balances. The start & endTime
// of each account increases progressively to ensure some accounts have locked balances, catering to the
// testing requirements.
//
// Parameters:
// - count: The number of regular accounts to generate.
//
// Returns:
// A GenesisOption function that takes the current genesis state as input, adds the generated accounts
// and balances to it, and returns the modified genesis state.
func (s *VestingModuleTestSuite) initContinuousVestingAccounts(count int) testnode.GenesisOption {
	names := testfactory.GenerateAccounts(count)
	s.accounts.Store(ContinuousVestingAccountType, _accountDispenser{names: names})
	bAccounts, balances := generateBaseAccounts(s.kr, names)

	initCoinsForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(initBalanceForGasFee))
	return func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		startTime := tmtime.Now()

		gAccounts := authtypes.GenesisAccounts{}
		for i := range bAccounts {
			bal := findBalanceByAddress(balances, bAccounts[i].GetAddress().String())
			assert.NotNil(s.T(), bal)
			coins := bal.Coins.Sub(initCoinsForGasFee)

			endTime := startTime.Add(10 * time.Second)
			acc := vestingtypes.NewContinuousVestingAccount(&bAccounts[i], coins, startTime.Unix(), endTime.Unix())
			gAccounts = append(gAccounts, acc)

			// the startTime is increased for each account to be able to test various scenarios
			startTime = startTime.Add(5 * time.Second)
		}

		var err error
		gs, err = AddAccountsToGenesisState(gs, gAccounts...)
		assert.NoError(s.T(), err)

		gs, err = AddBalancesToGenesisState(gs, balances...)
		assert.NoError(s.T(), err)

		return gs
	}
}

// unusedAccount returns an unused account name of the specified account type
// for the VestingModuleTestSuite. If the account type is not found, it panics
// with an error message.
//
// Parameters:
// - accType: The account type (_accountType) for which an unused account is requested.
//
// Returns:
// The next unused account of the specified account type.
func (s *VestingModuleTestSuite) unusedAccount(accType _accountType) string {
	s.accountsMut.Lock()
	defer s.accountsMut.Unlock()

	accountsAny, found := s.accounts.Load(accType)
	assert.True(s.T(), found, fmt.Sprintf("account type `%s` not found", accType.String()))

	accounts := accountsAny.(_accountDispenser)
	assert.Less(s.T(), accounts.counter, len(accounts.names), fmt.Sprintf("out of unused accounts for type `%s`", accType.String()))

	name := accounts.names[accounts.counter]
	accounts.counter++
	s.accounts.Store(accType, accounts)

	return name
}

func (t _accountType) String() string {
	switch t {
	case RegularAccountType:
		return "regular"
	case DelayedVestingAccountType:
		return "delayed vesting"
	case PeriodicVestingAccountType:
		return "periodic vesting"
	case ContinuousVestingAccountType:
		return "continuous vesting"
	default:
		return "unknown"
	}
}

// AddAccountsToGenesisState adds the provided accounts to the genesis state (gs) map for the auth module.
// It takes the raw genesis state (gs) and a variadic number of GenesisAccount objects (accounts) as inputs.
// Then, it updates the given genesis state and returns it.
func AddAccountsToGenesisState(gs map[string]json.RawMessage, accounts ...authtypes.GenesisAccount) (map[string]json.RawMessage, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	var authGenState authtypes.GenesisState
	err := encCfg.Codec.UnmarshalJSON(gs[authtypes.ModuleName], &authGenState)
	if err != nil {
		return gs, err
	}

	pAccs, err := authtypes.PackAccounts(accounts)
	if err != nil {
		return gs, err
	}

	// set the accounts in the genesis state
	authGenState.Accounts = append(authGenState.Accounts, pAccs...)
	gs[authtypes.ModuleName] = encCfg.Codec.MustMarshalJSON(&authGenState)

	return gs, nil
}

// AddBalancesToGenesisState adds the provided balances to the genesis state (gs) for the bank module.
// It takes the raw genesis state (gs) and a variadic number of Balance objects (balances) as inputs.
// It returns the updated gs and nil if the process is successful.
func AddBalancesToGenesisState(gs map[string]json.RawMessage, balances ...banktypes.Balance) (map[string]json.RawMessage, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	var bankGenState banktypes.GenesisState
	err := encCfg.Codec.UnmarshalJSON(gs[banktypes.ModuleName], &bankGenState)
	if err != nil {
		return gs, err
	}

	bankGenState.Balances = append(bankGenState.Balances, balances...)
	gs[banktypes.ModuleName] = encCfg.Codec.MustMarshalJSON(&bankGenState)

	return gs, nil
}

// GetValidators retrieves the validators from the staking module using the provided gRPC client connection (grpcConn).
// It takes a gRPC client connection (grpcConn) as input.
// Then, it returns the validators from the response.
func GetValidators(grpcConn *grpc.ClientConn) (stakingtypes.Validators, error) {
	scli := stakingtypes.NewQueryClient(grpcConn)
	vres, err := scli.Validators(context.Background(), &stakingtypes.QueryValidatorsRequest{})

	if vres == nil {
		return stakingtypes.Validators{}, err
	}
	return vres.Validators, err
}

// GetAccountDelegations retrieves the delegations for the specified account address using the provided gRPC client connection (grpcConn).
// It takes a gRPC client connection (grpcConn) and the account address (address) as inputs.
// If an error occurs during the request, it returns nil and the error.
// Otherwise, it returns the delegation responses from the response.
func GetAccountDelegations(grpcConn *grpc.ClientConn, address string) (stakingtypes.DelegationResponses, error) {
	cli := stakingtypes.NewQueryClient(grpcConn)
	res, err := cli.DelegatorDelegations(context.Background(),
		&stakingtypes.QueryDelegatorDelegationsRequest{DelegatorAddr: address})
	if err != nil {
		return nil, err
	}

	return res.DelegationResponses, err
}

// GetAccountSpendableBalance retrieves the spendable balance of an account for the specified address using gRPC.
// It takes a gRPC client connection (grpcConn) and the account address (address) as inputs.
// If the account is not found or an error occurs, it returns nil and the error.
// Otherwise, it returns the spendable balances of the account as an sdk.Coins object.
//
// Parameters:
// - grpcConn: A gRPC client connection.
// - address: The account address to retrieve the spendable balances for.
//
// Returns:
// The spendable balances of the account as an sdk.Coins object, or nil and an error if the retrieval fails.
func GetAccountSpendableBalance(grpcConn *grpc.ClientConn, address string) (sdk.Coins, error) {
	cli := banktypes.NewQueryClient(grpcConn)
	res, err := cli.SpendableBalances(
		context.Background(),
		&banktypes.QuerySpendableBalancesRequest{
			Address: address,
		},
	)
	if err != nil || res == nil {
		return nil, err
	}
	return res.GetBalances(), nil
}

// GetRawAccountInfo retrieves the raw account information for the specified address using gRPC.
// It takes a gRPC client connection (grpcConn) and the account address (address) as inputs.
// If no account found or an error occurs, it returns nil and the error.
// Otherwise, it returns the value field of the account in the response, which represents the raw account information.
//
// Parameters:
// - grpcConn: A gRPC client connection.
// - address: The account address to retrieve the raw information for.
//
// Returns:
// The raw account information as a byte slice, or nil and an error if the retrieval fails.
func GetRawAccountInfo(grpcConn *grpc.ClientConn, address string) ([]byte, error) {
	cli := authtypes.NewQueryClient(grpcConn)
	res, err := cli.Account(context.Background(), &authtypes.QueryAccountRequest{
		Address: address,
	})

	if err != nil || res == nil {
		return nil, err
	}

	return res.Account.Value, nil
}

// generateBaseAccounts generates base accounts and their corresponding balances.
// It takes a keyring.Keyring instance and a slice of account names as inputs.
// For each name in the names slice, it creates a new base account and adds it to
// the bAccounts slice. It also creates a banktypes.Balance struct for the account,
// with the account's address and coins including an initial coin for gas fee.
// The balances are added to the balances slice.
//
// Parameters:
// - kr: A keyring.Keyring instance used to create base accounts.
// - names: A slice of account names.
//
// Returns:
// - bAccounts: A slice of authtypes.BaseAccount representing the generated base accounts.
// - balances: A slice of banktypes.Balance representing the balances of the generated accounts.
func generateBaseAccounts(kr keyring.Keyring, names []string) ([]authtypes.BaseAccount, []banktypes.Balance) {
	initCoinsForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(initBalanceForGasFee))

	bAccounts := []authtypes.BaseAccount{}
	balances := []banktypes.Balance{}
	for _, name := range names {
		acc, coins := testfactory.NewBaseAccount(kr, name)
		bAccounts = append(bAccounts, *acc)
		balances = append(balances, banktypes.Balance{
			Address: acc.GetAddress().String(),
			Coins:   coins.Add(initCoinsForGasFee),
		})
	}

	return bAccounts, balances
}

// findBalanceByAddress finds a balance in the given slice of banktypes.Balance
// by matching the address with the provided address string.
// It iterates over the balances slice and returns the pointer to the first balance
// that has a matching address. If no matching balance is found, it returns nil.
//
// Parameters:
// - balances: A slice of banktypes.Balance to search within.
// - address: The address string to match against balance addresses.
//
// Returns:
// A pointer to the banktypes.Balance with a matching address, or nil if not found.
func findBalanceByAddress(balances []banktypes.Balance, address string) *banktypes.Balance {
	for _, b := range balances {
		if b.Address == address {
			return &b
		}
	}
	return nil
}
