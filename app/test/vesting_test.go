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
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	tmtime "github.com/tendermint/tendermint/types/time"
)

const (
	totalAccountsPerType = 300
	initBalanceForGasFee = 10
	vestingAmount        = testfactory.BaseAccountDefaultBalance
	vestingDelayPerTx    = 10 // this is a safe time to wait for a tx to be executed while the vesting period is not over yet
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

	// accounts is a map from accountType to accountDispenser
	accounts    sync.Map
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
func (s *VestingModuleTestSuite) startNewNetworkWithGenesisOpt(genesisOpts ...testnode.GenesisOption) {
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	cfg := testnode.DefaultConfig().WithGenesisOptions(testnode.ImmediateProposals(s.ecfg.Codec))
	cfg.GenesisOptions = genesisOpts

	cctx, _, _ := testnode.NewNetwork(s.T(), cfg)
	s.cctx = cctx
	s.cctx.Keyring = s.kr
}

func (s *VestingModuleTestSuite) TestGenesisDelayedVestingAccountsSpendableBalanceUnlocked() {
	block, err := s.cctx.LatestBlock()
	require.NoError(s.T(), err)
	// We need to wait for a block because sometimes querying based on the
	// latest height throws an SDK error saying the height is in the future
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find an unlocked delayed vesting account.
	vAcc, name, err := s.getAnUnusedDelayedVestingAccount(0)
	assert.NoError(s.T(), err)

	address := getAddress(name, s.cctx.Keyring).String()
	balances, err := testfactory.GetAccountSpendableBalanceByBlock(s.cctx.GRPCClient, address, block)
	assert.NoError(s.T(), err)

	expectedSpendableBal := vAcc.GetVestedCoins(block.Time).AmountOf(app.BondDenom).Int64() + initBalanceForGasFee
	assert.EqualValues(s.T(),
		expectedSpendableBal,
		balances.AmountOf(app.BondDenom).Int64(),
		"spendable balance must match")
}

func (s *VestingModuleTestSuite) TestGenesisDelayedVestingAccountsSpendableBalanceLocked() {
	block, err := s.cctx.LatestBlock()
	require.NoError(s.T(), err)
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find a delayed vesting account that its end-time has not reached yet i.e. locked
	vAcc, name, err := s.getAnUnusedDelayedVestingAccount(block.Time.Unix() + 10)
	assert.NoError(s.T(), err)

	address := getAddress(name, s.cctx.Keyring).String()
	balances, err := testfactory.GetAccountSpendableBalanceByBlock(s.cctx.GRPCClient, address, block)
	assert.NoError(s.T(), err)

	expectedSpendableBal := vAcc.GetVestedCoins(block.Time).AmountOf(app.BondDenom).Int64() + initBalanceForGasFee
	assert.EqualValues(s.T(),
		expectedSpendableBal,
		balances.AmountOf(app.BondDenom).Int64(),
		"spendable balance must match")
}

func (s *VestingModuleTestSuite) TestGenesisDelayedVestingAccountsTransfer() {
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account with endTime which is
	// at least 10 seconds away time from now to give the tx enough time to complete
	_, name, err := s.getAnUnusedDelayedVestingAccount(tmtime.Now().Unix() + vestingDelayPerTx)
	assert.NoError(s.T(), err)

	s.testTransferVestingAmount(name)
}

func (s *VestingModuleTestSuite) TestGenesisDelayedVestingAccountsDelegation() {
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account that has vesting (locked) balance
	_, name, err := s.getAnUnusedDelayedVestingAccount(tmtime.Now().Unix() + vestingDelayPerTx)
	assert.NoError(s.T(), err)

	s.testDelegatingVestingAmount(name)
}

func (s *VestingModuleTestSuite) TestGenesisDelayedVestingAccountsClaimDelegationRewards() {
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account with endTime which is
	_, name, err := s.getAnUnusedDelayedVestingAccount(tmtime.Now().Unix() + vestingDelayPerTx*2)
	assert.NoError(s.T(), err)

	s.testDelegatingVestingAmount(name)
	s.testClaimDelegationReward(name)
}

func (s *VestingModuleTestSuite) TestGenesisPeriodicVestingAccountsSpendableBalancePartiallyUnlocked() {
	// Find a periodic vesting account that has some vested (unlocked) balance (its start time has already passed)
	vAcc, name, err := s.getAnUnusedPeriodicVestingAccount(tmtime.Now().Unix() - 5)
	assert.NoError(s.T(), err)
	address := getAddress(name, s.cctx.Keyring).String()

	// Since we want a partially unlocked balance we need to wait until
	// the first period has passed if not already
	for vAcc.GetVestedCoins(tmtime.Now()).IsZero() {
		time.Sleep(time.Second)
	}

	block, err := s.cctx.LatestBlock()
	require.NoError(s.T(), err)
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	balances, err := testfactory.GetAccountSpendableBalanceByBlock(s.cctx.GRPCClient, address, block)
	assert.NoError(s.T(), err)

	expectedSpendableBal := vAcc.GetVestedCoins(block.Time).AmountOf(app.BondDenom).Int64() + initBalanceForGasFee
	assert.EqualValues(s.T(),
		expectedSpendableBal,
		balances.AmountOf(app.BondDenom).Int64(),
		"spendable balance must match")
}

func (s *VestingModuleTestSuite) TestGenesisPeriodicVestingAccountsSpendableBalanceLocked() {
	block, err := s.cctx.LatestBlock()
	require.NoError(s.T(), err)
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	// Find a periodic vesting account that that is currently in a vesting (locked) state
	// i.e. its start time yet to be reached.
	vAcc, name, err := s.getAnUnusedPeriodicVestingAccount(block.Time.Unix() + vestingDelayPerTx)
	assert.NoError(s.T(), err)
	address := getAddress(name, s.cctx.Keyring).String()

	balances, err := testfactory.GetAccountSpendableBalanceByBlock(s.cctx.GRPCClient, address, block)
	assert.NoError(s.T(), err)

	expectedSpendableBal := vAcc.GetVestedCoins(block.Time).AmountOf(app.BondDenom).Int64() + initBalanceForGasFee
	assert.EqualValues(s.T(),
		expectedSpendableBal,
		balances.AmountOf(app.BondDenom).Int64(),
		"spendable balance must match")
}

func (s *VestingModuleTestSuite) TestGenesisPeriodicVestingAccountsDelegation() {
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	// Find a periodic vesting account that that is currently in a vesting (locked) state
	// i.e. its start time yet to be reached.
	_, name, err := s.getAnUnusedPeriodicVestingAccount(tmtime.Now().Unix() + vestingDelayPerTx)
	assert.NoError(s.T(), err)

	s.testDelegatingVestingAmount(name)
}

// This test function tests delegation of a periodic vesting account that
// has part of its allocation unlocked and part of it locked
func (s *VestingModuleTestSuite) TestGenesisPeriodicVestingAccountsDelegationPartiallyVested() {
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account that has some vesting (locked) and
	// some vested (unlocked) balance
	vAcc, name, err := s.getAnUnusedPeriodicVestingAccount(tmtime.Now().Unix() - 5)
	assert.NoError(s.T(), err)

	// Since we want a partially unlocked balance we need to wait until
	// the first period has passed if not already
	for vAcc.GetVestedCoins(tmtime.Now()).IsZero() {
		time.Sleep(time.Second)
	}
	s.testDelegatingVestingAmount(name)
}

func (s *VestingModuleTestSuite) TestGenesisPeriodicVestingAccountsClaimDelegationRewards() {
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account that has some vesting (locked) balance
	// to be on the safe side we select one that starts unlocking in at least vestingDelayPerTx*2 seconds
	_, name, err := s.getAnUnusedPeriodicVestingAccount(tmtime.Now().Unix() + vestingDelayPerTx*2)
	assert.NoError(s.T(), err)

	s.testDelegatingVestingAmount(name)
	s.testClaimDelegationReward(name)
}

func (s *VestingModuleTestSuite) TestGenesisContinuousVestingAccountsSpendableBalanceLocked() {
	block, err := s.cctx.LatestBlock()
	require.NoError(s.T(), err)
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find a continuous vesting account with locked balance
	vAcc, name, err := s.getAnUnusedContinuousVestingAccount(block.Time.Unix() + vestingDelayPerTx)
	assert.NoError(s.T(), err)
	address := getAddress(name, s.cctx.Keyring).String()

	balances, err := testfactory.GetAccountSpendableBalanceByBlock(s.cctx.GRPCClient, address, block)
	assert.NoError(s.T(), err)

	expectedSpendableBalAmount := vAcc.GetVestedCoins(block.Time).AmountOf(app.BondDenom).Int64() + initBalanceForGasFee
	assert.EqualValues(s.T(),
		expectedSpendableBalAmount,
		balances.AmountOf(app.BondDenom).Int64(),
	)
}

func (s *VestingModuleTestSuite) TestGenesisContinuousVestingAccountsSpendableBalancePartiallyUnlocked() {
	// find a continuous vesting account with partially unlocked balance
	vAcc, name, err := s.getAnUnusedContinuousVestingAccount(tmtime.Now().Unix() - 5)
	assert.NoError(s.T(), err)
	address := getAddress(name, s.cctx.Keyring).String()

	// Since we want a partially unlocked balance we need to wait until
	// the start time just passes if not already
	for vAcc.GetVestedCoins(tmtime.Now()).IsZero() {
		time.Sleep(time.Second)
	}

	block, err := s.cctx.LatestBlock()
	require.NoError(s.T(), err)
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	balances, err := testfactory.GetAccountSpendableBalanceByBlock(s.cctx.GRPCClient, address, block)
	assert.NoError(s.T(), err)

	expectedSpendableBalAmount := vAcc.GetVestedCoins(block.Time).AmountOf(app.BondDenom).Int64() + initBalanceForGasFee
	assert.EqualValues(s.T(),
		expectedSpendableBalAmount,
		balances.AmountOf(app.BondDenom).Int64(),
	)
}

func (s *VestingModuleTestSuite) TestGenesisContinuousVestingAccountsDelegation() {
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account that has some vesting (locked) balance
	_, name, err := s.getAnUnusedContinuousVestingAccount(tmtime.Now().Unix() + vestingDelayPerTx)
	assert.NoError(s.T(), err)

	s.testDelegatingVestingAmount(name)
}

// This test function tests delegation of a continuous vesting account that
// has part of its allocation unlocked and part of it locked
func (s *VestingModuleTestSuite) TestGenesisContinuousVestingAccountsDelegationPartiallyVested() {
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find a continuous vesting account with partially unlocked balance
	vAcc, name, err := s.getAnUnusedContinuousVestingAccount(tmtime.Now().Unix() - 5)
	assert.NoError(s.T(), err)

	// Since we want a partially unlocked balance we need to wait until
	// the start time just passes if not already
	for vAcc.GetVestedCoins(tmtime.Now()).IsZero() {
		time.Sleep(time.Second)
	}
	s.testDelegatingVestingAmount(name)
}

func (s *VestingModuleTestSuite) TestGenesisContinuousVestingAccountsClaimDelegationRewards() {
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account that has some vesting (locked) balance
	// to be on the safe side we select one that starts unlocking in at least vestingDelayPerTx*2 seconds
	_, name, err := s.getAnUnusedContinuousVestingAccount(tmtime.Now().Unix() + vestingDelayPerTx*2)
	assert.NoError(s.T(), err)

	s.testDelegatingVestingAmount(name)
	s.testClaimDelegationReward(name)
}

// testTransferVestingAmount tests the transfer of vesting amounts (locked balance) from a vesting account
// to another account. It takes the name of the vesting account (name) as an input.
// It retrieves a random unused regular account and attempts to transfer the locked amount from the vesting
// account to the random account. It asserts that the result code of the transaction is equal to 5,
// indicating a failure in the transfer.
func (s *VestingModuleTestSuite) testTransferVestingAmount(name string) {
	randomAcc, err := s.unusedAccount(RegularAccountType)
	assert.NoError(s.T(), err)

	msgSend := banktypes.NewMsgSend(
		getAddress(name, s.cctx.Keyring),
		getAddress(randomAcc, s.cctx.Keyring),
		sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(vestingAmount))), // try to transfer the locked amount
	)
	resTx, err := testnode.SignAndBroadcastTx(s.ecfg, s.cctx.Context, name, []sdk.Msg{msgSend}...)
	require.NoError(s.T(), err)

	resQ, err := s.cctx.WaitForTx(resTx.TxHash, 10)
	require.NoError(s.T(), err)

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
	require.NoError(s.T(), err)

	resQ, err := s.cctx.WaitForTx(resTx.TxHash, 10)
	require.NoError(s.T(), err)
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
	assert.NoError(s.T(), s.cctx.WaitForBlocks(5))

	address := getAddress(name, s.cctx.Keyring).String()

	cli := distributiontypes.NewQueryClient(s.cctx.GRPCClient)
	resR, err := cli.DelegationTotalRewards(
		context.Background(),
		&distributiontypes.QueryDelegationTotalRewardsRequest{
			DelegatorAddress: address,
		},
	)
	require.NoError(s.T(), err)
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
	require.NoError(s.T(), err)

	resQ, err := s.cctx.WaitForTx(resTx.TxHash, 10)
	require.NoError(s.T(), err)
	assert.EqualValues(s.T(), 0, resQ.TxResult.Code, fmt.Sprintf("the claim reward TX must succeed: \n%s", resQ.TxResult.String()))

	require.NoError(s.T(), s.cctx.WaitForNextBlock())

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
func (s *VestingModuleTestSuite) initRegularAccounts(count int) testnode.GenesisOption {
	names := testfactory.GenerateAccounts(count)
	s.accounts.Store(RegularAccountType, accountDispenser{names: names})
	bAccounts, balances := testfactory.GenerateBaseAccounts(s.kr, names)
	gAccounts := authtypes.GenesisAccounts{}
	for i := range bAccounts {
		gAccounts = append(gAccounts, authtypes.GenesisAccount(&bAccounts[i]))
	}

	return func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		gs, err := AddGenesisAccountsWithBalancesToGenesisState(gs, gAccounts, balances)
		assert.NoError(s.T(), err)
		return gs
	}
}

// initDelayedVestingAccounts initializes delayed vesting accounts for the VestingModuleTestSuite.
// It generates the specified number of account names and stores them in the accounts map with
// RegularAccountType as the key.
func (s *VestingModuleTestSuite) initDelayedVestingAccounts(count int) testnode.GenesisOption {
	initCoinsForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(initBalanceForGasFee))

	names := testfactory.GenerateAccounts(count)
	s.accounts.Store(DelayedVestingAccountType, accountDispenser{names: names})
	vAccounts, balances := testfactory.GenerateDelayedVestingAccounts(s.kr, names, initCoinsForGasFee)
	gAccounts := authtypes.GenesisAccounts{}
	for i := range vAccounts {
		gAccounts = append(gAccounts, authtypes.GenesisAccount(vAccounts[i]))
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
func (s *VestingModuleTestSuite) initPeriodicVestingAccounts(count int) testnode.GenesisOption {
	initCoinsForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(initBalanceForGasFee))

	names := testfactory.GenerateAccounts(count)
	s.accounts.Store(PeriodicVestingAccountType, accountDispenser{names: names})
	vAccounts, balances := testfactory.GeneratePeriodicVestingAccounts(s.kr, names, initCoinsForGasFee)
	gAccounts := authtypes.GenesisAccounts{}
	for i := range vAccounts {
		gAccounts = append(gAccounts, authtypes.GenesisAccount(vAccounts[i]))
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
func (s *VestingModuleTestSuite) initContinuousVestingAccounts(count int) testnode.GenesisOption {
	initCoinsForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(initBalanceForGasFee))
	names := testfactory.GenerateAccounts(count)
	s.accounts.Store(ContinuousVestingAccountType, accountDispenser{names: names})

	vAccounts, balances := testfactory.GenerateContinuousVestingAccounts(s.kr, names, initCoinsForGasFee)
	gAccounts := authtypes.GenesisAccounts{}
	for i := range vAccounts {
		gAccounts = append(gAccounts, authtypes.GenesisAccount(vAccounts[i]))
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
func (s *VestingModuleTestSuite) unusedAccount(accType accountType) (string, error) {
	s.accountsMut.Lock()
	defer s.accountsMut.Unlock()

	accountsAny, found := s.accounts.Load(accType)
	if !found {
		return "", fmt.Errorf("account type `%s` not found", accType.String())
	}

	accounts := accountsAny.(accountDispenser)
	if accounts.counter >= len(accounts.names) {
		return "", fmt.Errorf("out of unused accounts for type `%s`", accType.String())
	}

	name := accounts.names[accounts.counter]
	accounts.counter++
	s.accounts.Store(accType, accounts)

	return name, nil
}

// getAnUnusedContinuousVestingAccount returns an unused continuous vesting account and its name.
//
// It takes a minimum start-time as input and finds an unused account whose start time is greater than the input.
func (s *VestingModuleTestSuite) getAnUnusedContinuousVestingAccount(minStartTime int64) (vAcc vestingtypes.ContinuousVestingAccount, name string, err error) {
	for {
		name, err = s.unusedAccount(ContinuousVestingAccountType)
		if err != nil {
			return vAcc, name, err
		}
		address := getAddress(name, s.cctx.Keyring).String()
		resAccBytes, err := testfactory.GetRawAccountInfo(s.cctx.GRPCClient, address)
		if err != nil {
			return vAcc, name, err
		}

		err = vAcc.Unmarshal(resAccBytes)
		if err != nil || vAcc.StartTime > minStartTime {
			return vAcc, name, err
		}
	}
}

// getAnUnusedPeriodicVestingAccount returns an unused periodic vesting account and its name.
//
// It takes a minimum start-time as input and finds an unused account whose start time is greater than the input.
func (s *VestingModuleTestSuite) getAnUnusedPeriodicVestingAccount(minStartTime int64) (vAcc vestingtypes.PeriodicVestingAccount, name string, err error) {
	for {
		name, err = s.unusedAccount(PeriodicVestingAccountType)
		if err != nil {
			return vAcc, name, err
		}
		address := getAddress(name, s.cctx.Keyring).String()
		resAccBytes, err := testfactory.GetRawAccountInfo(s.cctx.GRPCClient, address)
		if err != nil {
			return vAcc, name, err
		}

		err = vAcc.Unmarshal(resAccBytes)
		if err != nil || vAcc.StartTime > minStartTime {
			return vAcc, name, err
		}
	}
}

// getAnUnusedDelayedVestingAccount returns the name of an unused delayed vesting account.
//
// It takes a minimum end-time as input and finds an unused account whose end time is greater than the input.
func (s *VestingModuleTestSuite) getAnUnusedDelayedVestingAccount(minEndTime int64) (vAcc vestingtypes.DelayedVestingAccount, name string, err error) {
	for {
		name, err = s.unusedAccount(DelayedVestingAccountType)
		if err != nil {
			return vAcc, name, err
		}

		address := getAddress(name, s.cctx.Keyring).String()
		resAccBytes, err := testfactory.GetRawAccountInfo(s.cctx.GRPCClient, address)
		if err != nil {
			return vAcc, name, err
		}

		err = vAcc.Unmarshal(resAccBytes)
		if err != nil || vAcc.EndTime > minEndTime {
			return vAcc, name, err
		}
	}
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
