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
)

const (
	totalAccountsPerType = 300
	initBalanceForGasFee = 10
	vestingAmount        = testfactory.BaseAccountDefaultBalance
)

type accountDispenser struct {
	names   []string
	counter int
}

type accountType int

const (
	RegularAccountType accountType = iota + 1
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
	ecfg encoding.Config
}

func TestVestingModule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Vesting accounts test in short mode.")
	}
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

// startNewNetworkWithGenesisOpt creates a new test network with the specified genesis options for the VestingModuleTestSuite.
// It initializes a default Tendermint configuration (tmCfg) and default application configuration (appConf).
// The target block time is set to 1 millisecond. It applies the given genesis options to the test network
// and stores the created client context (cctx) in the VestingModuleTestSuite.
// The keyring of the context is set to the keyring (s.kr) of the VestingModuleTestSuite.
//
// Parameters:
// - genesisOpts: The genesis options to be applied when creating the test network.
func (s *VestingModuleTestSuite) startNewNetworkWithGenesisOpt(genesisOpts ...testnode.GenesisOption) {
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	cfg := testnode.DefaultConfig().WithGenesisOptions(testnode.ImmediateProposals(s.ecfg.Codec))
	cfg.GenesisOptions = genesisOpts

	cctx, _, _ := testnode.NewNetwork(s.T(), cfg)
	s.cctx = cctx
	s.cctx.Keyring = s.kr
}

func (s *VestingModuleTestSuite) TestGenesisDelayedVestingAccountsSpendableBalance() {
	assert.NoError(s.T(), s.cctx.WaitForNextBlock())

	for {
		vAcc, name, err := s.getAnUnusedDelayedVestingAccount()
		assert.NoError(s.T(), err)
		address := getAddress(name, s.cctx.Keyring).String()

		alreadyVested := vAcc.EndTime < tmtime.Now().Unix()

		balances, err := testfactory.GetAccountSpendableBalance(s.cctx.GRPCClient, address)
		assert.NoError(s.T(), err)
		expectedSpendableBal := initBalanceForGasFee
		if alreadyVested {
			expectedSpendableBal += vestingAmount
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
	// at least 10 seconds away time from now
	for {
		vAcc, name, err := s.getAnUnusedDelayedVestingAccount()
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
		vAcc, name, err := s.getAnUnusedDelayedVestingAccount()
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
		vAcc, name, err := s.getAnUnusedDelayedVestingAccount()
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
		vAcc, name, err := s.getAnUnusedPeriodicVestingAccount()
		assert.NoError(s.T(), err)
		address := getAddress(name, s.cctx.Keyring).String()

		balances, err := testfactory.GetAccountSpendableBalance(s.cctx.GRPCClient, address)
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
		vAcc, name, err := s.getAnUnusedPeriodicVestingAccount()
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
		vAcc, name, err := s.getAnUnusedPeriodicVestingAccount()
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
	// to be on the safe side we select one that starts unlocking in at least 20 seconds
	for {
		vAcc, name, err := s.getAnUnusedPeriodicVestingAccount()
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
		vAcc, name, err := s.getAnUnusedContinuousVestingAccount()
		assert.NoError(s.T(), err)
		address := getAddress(name, s.cctx.Keyring).String()

		balances, err := testfactory.GetAccountSpendableBalance(s.cctx.GRPCClient, address)
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
		vAcc, name, err := s.getAnUnusedContinuousVestingAccount()
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
		vAcc, name, err := s.getAnUnusedContinuousVestingAccount()
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
		vAcc, name, err := s.getAnUnusedContinuousVestingAccount()
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
		sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(vestingAmount))), // try to transfer the locked amount
	)
	resTx, err := testnode.SignAndBroadcastTx(s.ecfg, s.cctx.Context, name, []sdk.Msg{msgSend}...)
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

	del, err := testfactory.GetAccountDelegations(s.cctx.GRPCClient, address)
	assert.NoError(s.T(), err)
	assert.Empty(s.T(), del, "initial delegation must be empty")

	validators, err := testfactory.GetValidators(s.cctx.GRPCClient)
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), validators)

	msgDelg := stakingtypes.NewMsgDelegate(
		getAddress(name, s.cctx.Keyring),
		validators[0].GetOperator(),
		sdk.NewCoin(app.BondDenom, sdk.NewInt(vestingAmount)),
	)
	resTx, err := testnode.SignAndBroadcastTx(s.ecfg, s.cctx.Context, name, []sdk.Msg{msgDelg}...)
	assert.NoError(s.T(), err)

	resQ, err := s.cctx.WaitForTx(resTx.TxHash, 10)
	assert.NoError(s.T(), err)
	assert.EqualValues(s.T(), 0, resQ.TxResult.Code, fmt.Sprintf("the delegation TX must succeed: \n%s", resQ.TxResult.String()))

	// verify the delegations
	del, err = testfactory.GetAccountDelegations(s.cctx.GRPCClient, address)
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), del, "delegations must not be empty")
	assert.EqualValues(s.T(),
		vestingAmount,
		del[0].Balance.Amount.Int64(),
		"delegation amount must match")
}

// testClaimDelegationReward tests the claiming of delegation rewards for a vesting account.
// It takes the name of the vesting account (name) as an input.
// It claims the delegation rewards and then retrieves the balances of the vesting account.
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

	balancesBefore, err := testfactory.GetAccountSpendableBalance(s.cctx.GRPCClient, address)
	assert.NoError(s.T(), err)

	// minExpectedBalance is used because more tokens may be vested to the
	// account in the middle of this test
	minExpectedBalance := balancesBefore.AmountOf(app.BondDenom).Int64() + rewardAmount

	validators, err := testfactory.GetValidators(s.cctx.GRPCClient)
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), validators, "empty validators set")

	msg := distributiontypes.NewMsgWithdrawDelegatorReward(
		getAddress(name, s.cctx.Keyring),
		validators[0].GetOperator(),
	)
	resTx, err := testnode.SignAndBroadcastTx(s.ecfg, s.cctx.Context, name, []sdk.Msg{msg}...)
	assert.NoError(s.T(), err)

	resQ, err := s.cctx.WaitForTx(resTx.TxHash, 10)
	assert.NoError(s.T(), err)
	assert.EqualValues(s.T(), 0, resQ.TxResult.Code, fmt.Sprintf("the claim reward TX must succeed: \n%s", resQ.TxResult.String()))

	// Check if the reward amount in the account
	balancesAfter, err := testfactory.GetAccountSpendableBalance(s.cctx.GRPCClient, address)
	assert.NoError(s.T(), err)

	assert.GreaterOrEqual(s.T(), balancesAfter.AmountOf(app.BondDenom).Int64(), minExpectedBalance)
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
	s.accounts.Store(RegularAccountType, accountDispenser{names: names})
	bAccounts, balances := testfactory.GenerateBaseAccounts(s.kr, names)
	gAccounts := authtypes.GenesisAccounts{}
	for i := range bAccounts {
		gAccounts = append(gAccounts, &bAccounts[i])
	}

	return func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		gs, err := AddGenesisAccountsWithBalancesToGenesisState(gs, gAccounts, balances)
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
	initCoinsForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(initBalanceForGasFee))

	names := testfactory.GenerateAccounts(count)
	s.accounts.Store(DelayedVestingAccountType, accountDispenser{names: names})
	vAccounts, balances := testfactory.GenerateDelayedVestingAccounts(s.kr, names, initCoinsForGasFee)
	gAccounts := authtypes.GenesisAccounts{}
	for i := range vAccounts {
		gAccounts = append(gAccounts, vAccounts[i])
	}

	return func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		gs, err := AddGenesisAccountsWithBalancesToGenesisState(gs, gAccounts, balances)
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
	initCoinsForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(initBalanceForGasFee))

	names := testfactory.GenerateAccounts(count)
	s.accounts.Store(PeriodicVestingAccountType, accountDispenser{names: names})
	vAccounts, balances := testfactory.GeneratePeriodicVestingAccounts(s.kr, names, initCoinsForGasFee)
	gAccounts := authtypes.GenesisAccounts{}
	for i := range vAccounts {
		gAccounts = append(gAccounts, vAccounts[i])
	}

	return func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		gs, err := AddGenesisAccountsWithBalancesToGenesisState(gs, gAccounts, balances)
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
	initCoinsForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(initBalanceForGasFee))
	names := testfactory.GenerateAccounts(count)
	s.accounts.Store(ContinuousVestingAccountType, accountDispenser{names: names})

	vAccounts, balances := testfactory.GenerateContinuousVestingAccounts(s.kr, names, initCoinsForGasFee)
	gAccounts := authtypes.GenesisAccounts{}
	for i := range vAccounts {
		gAccounts = append(gAccounts, vAccounts[i])
	}

	return func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		gs, err := AddGenesisAccountsWithBalancesToGenesisState(gs, gAccounts, balances)
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
func (s *VestingModuleTestSuite) unusedAccount(accType accountType) string {
	s.accountsMut.Lock()
	defer s.accountsMut.Unlock()

	accountsAny, found := s.accounts.Load(accType)
	assert.True(s.T(), found, fmt.Sprintf("account type `%s` not found", accType.String()))

	accounts := accountsAny.(accountDispenser)
	assert.Less(s.T(), accounts.counter, len(accounts.names), fmt.Sprintf("out of unused accounts for type `%s`", accType.String()))

	name := accounts.names[accounts.counter]
	accounts.counter++
	s.accounts.Store(accType, accounts)

	return name
}

// getAnUnusedContinuousVestingAccount retrieves an unused continuous vesting account.
func (s *VestingModuleTestSuite) getAnUnusedContinuousVestingAccount() (vAcc vestingtypes.ContinuousVestingAccount, name string, err error) {
	name = s.unusedAccount(ContinuousVestingAccountType)
	address := getAddress(name, s.cctx.Keyring).String()
	resAccBytes, err := testfactory.GetRawAccountInfo(s.cctx.GRPCClient, address)
	if err != nil {
		return vAcc, name, err
	}

	err = vAcc.Unmarshal(resAccBytes)
	return vAcc, name, err
}

func (s *VestingModuleTestSuite) getAnUnusedPeriodicVestingAccount() (vAcc vestingtypes.PeriodicVestingAccount, name string, err error) {
	name = s.unusedAccount(PeriodicVestingAccountType)
	address := getAddress(name, s.cctx.Keyring).String()
	resAccBytes, err := testfactory.GetRawAccountInfo(s.cctx.GRPCClient, address)
	if err != nil {
		return vAcc, name, err
	}

	err = vAcc.Unmarshal(resAccBytes)
	return vAcc, name, err
}

func (s *VestingModuleTestSuite) getAnUnusedDelayedVestingAccount() (vAcc vestingtypes.DelayedVestingAccount, name string, err error) {
	name = s.unusedAccount(DelayedVestingAccountType)
	address := getAddress(name, s.cctx.Keyring).String()
	resAccBytes, err := testfactory.GetRawAccountInfo(s.cctx.GRPCClient, address)
	if err != nil {
		return vAcc, name, err
	}

	err = vAcc.Unmarshal(resAccBytes)
	return vAcc, name, err
}

func (t accountType) String() string {
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

// AddGenesisAccountsWithBalancesToGenesisState adds the given genesis accounts and balances to the
// provided genesis state. It returns the updated genesis state and an error if any occurred.
//
// Parameters:
// - gs: A map representing the current genesis state.
// - gAccounts: A slice of genesis accounts to be added.
// - balances: A slice of balances to be added.
//
// Returns:
// - gs: The updated genesis state after adding the accounts and balances.
// - error: An error if any occurred during the process.
func AddGenesisAccountsWithBalancesToGenesisState(
	gs map[string]json.RawMessage,
	gAccounts []authtypes.GenesisAccount,
	balances []banktypes.Balance,
) (map[string]json.RawMessage, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	var err error
	gs, err = testfactory.AddAccountsToGenesisState(encCfg, gs, gAccounts...)
	if err != nil {
		return gs, err
	}

	gs, err = testfactory.AddBalancesToGenesisState(encCfg, gs, balances)
	if err != nil {
		return gs, err
	}

	return gs, nil
}
