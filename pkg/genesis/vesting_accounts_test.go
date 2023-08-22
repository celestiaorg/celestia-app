package genesis_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/genesis"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	tmtime "github.com/tendermint/tendermint/types/time"
)

const (
	totalAccountsPerType = 300
	initBalanceForGasFee = 10
	vestingAmount        = testfactory.BaseAccountDefaultBalance
	vestingDelayPerTx    = 10 // this is a safe time (in seconds) to wait for a tx to be executed while the vesting period is not over yet
	testTimeout          = time.Minute
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
		t.Skip("skipping vesting module test suite in short mode.")
	}
	suite.Run(t, new(VestingModuleTestSuite))
}

func (s *VestingModuleTestSuite) SetupSuite() {
	t := s.T()
	t.Log("setting up vesting module test suite")

	s.kr, _ = testnode.NewKeyring()

	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	cfg := testnode.DefaultConfig().WithGenesisOptions(testnode.ImmediateProposals(s.ecfg.Codec))

	cfg.GenesisOptions = []testnode.GenesisOption{
		s.initRegularAccounts(totalAccountsPerType),
		s.initDelayedVestingAccounts(totalAccountsPerType),
		s.initPeriodicVestingAccounts(totalAccountsPerType),
		s.initContinuousVestingAccounts(totalAccountsPerType),
	}

	s.cctx, _, _ = testnode.NewNetwork(s.T(), cfg)
	s.cctx.Keyring = s.kr
}

func (s *VestingModuleTestSuite) TestGenesisDelayedVestingAccountsTransferLocked() {
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	// find and test a vesting account with endTime which is
	// at least 10 seconds away from now to give the tx enough time to complete
	_, name, err := s.getAnUnusedDelayedVestingAccount(tmtime.Now().Unix() + vestingDelayPerTx)
	require.NoError(s.T(), err)

	subCtx, cancel := context.WithTimeout(s.cctx.GoContext(), testTimeout)
	defer cancel()

	s.testTransferMustFail(subCtx, name, vestingAmount)
}

func (s *VestingModuleTestSuite) TestGenesisDelayedVestingAccountsTransferUnLocked() {
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	subCtx, cancel := context.WithTimeout(s.cctx.GoContext(), testTimeout)
	defer cancel()

	// find and test a vesting account with endTime which is already passed
	vAcc, name, err := s.getAnUnusedDelayedVestingAccount(0)
	require.NoError(s.T(), err)

	// Since we want a partially unlocked balance we need to wait until
	// the endTime is passed if not already
	ticker := time.NewTicker(time.Second)
	for vAcc.GetVestedCoins(tmtime.Now()).IsZero() {
		select {
		case <-subCtx.Done():
			s.T().Fatalf("test timeout exceeded: expected vested coins to be non-zero")
		case <-ticker.C:
			continue
		}
	}

	minExpectedSpendableBal := vAcc.GetVestedCoins(tmtime.Now()).AmountOf(app.BondDenom).Int64()
	require.NotZero(s.T(), minExpectedSpendableBal)
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	// it must be able to transfer the entire vesting amount
	s.testTransferMustSucceed(subCtx, name, minExpectedSpendableBal)
}

func (s *VestingModuleTestSuite) TestGenesisPeriodicVestingAccountsTransferPartiallyUnlocked() {
	// Find a periodic vesting account that has some vested (unlocked) balance (its start time has already passed)
	vAcc, name, err := s.getAnUnusedPeriodicVestingAccount(tmtime.Now().Unix() - 5)
	require.NoError(s.T(), err)

	subCtx, cancel := context.WithTimeout(s.cctx.GoContext(), testTimeout)
	defer cancel()

	// Since we want a partially unlocked balance we need to wait until
	// the first period has passed if not already
	ticker := time.NewTicker(time.Second)
	for vAcc.GetVestedCoins(tmtime.Now()).IsZero() {
		select {
		case <-subCtx.Done():
			s.T().Fatalf("test timeout exceeded: expected vested coins to be non-zero")
		case <-ticker.C:
			continue
		}
	}

	minExpectedSpendableBal := vAcc.GetVestedCoins(tmtime.Now()).AmountOf(app.BondDenom).Int64()
	require.NotZero(s.T(), minExpectedSpendableBal)
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	s.testTransferMustSucceed(subCtx, name, minExpectedSpendableBal)
}

func (s *VestingModuleTestSuite) TestGenesisPeriodicVestingAccountsTransferLocked() {
	// Find a periodic vesting account that that is currently in a vesting (locked) state
	// i.e. its start time yet to be reached.
	vAcc, name, err := s.getAnUnusedPeriodicVestingAccount(tmtime.Now().Unix() + vestingDelayPerTx)
	require.NoError(s.T(), err)
	require.Zero(s.T(), vAcc.GetVestedCoins(tmtime.Now()).AmountOf(app.BondDenom).Int64())

	subCtx, cancel := context.WithTimeout(s.cctx.GoContext(), testTimeout)
	defer cancel()

	s.testTransferMustFail(subCtx, name, vestingAmount)
}

func (s *VestingModuleTestSuite) TestGenesisContinuousVestingAccountsTransferLocked() {
	// find a continuous vesting account with locked balance
	vAcc, name, err := s.getAnUnusedContinuousVestingAccount(tmtime.Now().Unix() + vestingDelayPerTx)
	require.NoError(s.T(), err)
	require.Zero(s.T(), vAcc.GetVestedCoins(tmtime.Now()).AmountOf(app.BondDenom).Int64())

	subCtx, cancel := context.WithTimeout(s.cctx.GoContext(), testTimeout)
	defer cancel()

	s.testTransferMustFail(subCtx, name, vestingAmount)
}

func (s *VestingModuleTestSuite) TestGenesisContinuousVestingAccountsTransferPartiallyUnlocked() {
	// find a continuous vesting account with partially unlocked balance
	vAcc, name, err := s.getAnUnusedContinuousVestingAccount(tmtime.Now().Unix() - 5)
	require.NoError(s.T(), err)

	subCtx, cancel := context.WithTimeout(s.cctx.GoContext(), testTimeout)
	defer cancel()

	// Since we want a partially unlocked balance we need to wait until
	// the start time just passes if not already
	ticker := time.NewTicker(time.Second)
	for vAcc.GetVestedCoins(tmtime.Now()).IsZero() {
		select {
		case <-subCtx.Done():
			s.T().Fatalf("test timeout exceeded: expected vested coins to be non-zero")
		case <-ticker.C:
			continue
		}
	}

	minExpectedSpendableBal := vAcc.GetVestedCoins(tmtime.Now()).AmountOf(app.BondDenom).Int64()
	require.NotZero(s.T(), minExpectedSpendableBal)
	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	s.testTransferMustSucceed(subCtx, name, minExpectedSpendableBal)
}

// testTransferMustFail tests the transfer of an amount (which must be locked to fail)
// from a vesting account to another account. It asserts that the result code of the
// transaction is equal to 5, indicating an InsufficientFunds error.
func (s *VestingModuleTestSuite) testTransferMustFail(ctx context.Context, name string, amount int64) {
	txResultCode, err := s.submitTransferTx(ctx, name, amount)
	require.Error(s.T(), err, "transfer should fail")
	assert.EqualValues(s.T(), sdkerrors.ErrInsufficientFunds.ABCICode(), txResultCode, "tranfer should fail")
}

// testTransferMustSucceed tests the transfer of a certain amount of funds from one account
// to another. It asserts that the result code of the transaction is equal to 0, indicating
// a success.
func (s *VestingModuleTestSuite) testTransferMustSucceed(ctx context.Context, name string, amount int64) {
	txResultCode, err := s.submitTransferTx(ctx, name, amount)
	require.NoError(s.T(), err, "transfer should succeed")
	assert.EqualValues(s.T(), abci.CodeTypeOK, txResultCode, "transfer should succeed")
}

// submitTransferTx submits a transfer transaction to a random account and returns the tx result code
func (s *VestingModuleTestSuite) submitTransferTx(ctx context.Context, name string, amount int64) (txResultCode uint32, err error) {
	randomAcc, err := s.unusedAccount(RegularAccountType)
	if err != nil {
		return 0, err
	}

	addr := testfactory.GetAddress(s.cctx.Keyring, name)
	msgSend := banktypes.NewMsgSend(
		addr,
		testfactory.GetAddress(s.cctx.Keyring, randomAcc),
		sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(amount))),
	)

	signer, err := user.SetupSigner(ctx, s.cctx.Keyring, s.cctx.GRPCClient, addr, s.ecfg)
	if err != nil {
		return 0, err
	}

	resTx, err := signer.SubmitTx(ctx, []sdk.Msg{msgSend}, user.SetGasLimit(1e6), user.SetFee(1))
	if err != nil {
		return resTx.Code, err
	}

	return resTx.Code, nil
}

// initRegularAccounts initializes regular accounts for the VestingModuleTestSuite.
// It generates the specified number of account names and stores them in the accounts map
// of the VestingModuleTestSuite with RegularAccountType as the key. It also generates base accounts
// and their default balances. The generated accounts and balances are added to the provided genesis
// state. The genesis state modifier function is returned.
func (s *VestingModuleTestSuite) initRegularAccounts(count int) testnode.GenesisOption {
	names := testfactory.GenerateAccounts(count)
	s.accounts.Store(RegularAccountType, accountDispenser{names: names})

	gAccounts := authtypes.GenesisAccounts{}
	balances := []banktypes.Balance{}

	for i := range names {
		bAccount, defaultBalance := testfactory.NewBaseAccount(s.kr, names[i])
		gAccount, balance, err := genesis.NewGenesisRegularAccount(
			bAccount.GetAddress().String(),
			defaultBalance,
		)
		require.NoError(s.T(), err)

		gAccounts = append(gAccounts, gAccount)
		balances = append(balances, balance)
	}

	return func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		gs, err := genesis.AddGenesisAccountsWithBalancesToGenesisState(s.ecfg, gs, gAccounts, balances)
		assert.NoError(s.T(), err)
		return gs
	}
}

// initDelayedVestingAccounts initializes delayed vesting accounts for the VestingModuleTestSuite.
// It generates the specified number of account names and stores them in the accounts map with
// DelayedVestingAccountType as the key.
func (s *VestingModuleTestSuite) initDelayedVestingAccounts(count int) testnode.GenesisOption {
	initCoinsForGasFee := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(initBalanceForGasFee)))

	names := testfactory.GenerateAccounts(count)
	s.accounts.Store(DelayedVestingAccountType, accountDispenser{names: names})

	gAccounts := authtypes.GenesisAccounts{}
	balances := []banktypes.Balance{}

	endTime := tmtime.Now().Add(-2 * time.Second)
	for i := range names {
		bAccount, defaultBalance := testfactory.NewBaseAccount(s.kr, names[i])
		gAccount, balance, err := genesis.NewGenesisDelayedVestingAccount(
			bAccount.GetAddress().String(),
			defaultBalance,
			initCoinsForGasFee,
			endTime)
		require.NoError(s.T(), err)

		gAccounts = append(gAccounts, gAccount)
		balances = append(balances, balance)

		// the endTime is increased for each account to be able to test various scenarios
		endTime = endTime.Add(5 * time.Second)
	}

	return func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		gs, err := genesis.AddGenesisAccountsWithBalancesToGenesisState(s.ecfg, gs, gAccounts, balances)
		require.NoError(s.T(), err)
		return gs
	}
}

// initPeriodicVestingAccounts function initializes periodic vesting accounts for testing purposes.
// It takes the count of accounts as input and returns a GenesisOption. It generates account names,
// base accounts, and balances. It defines vesting periods and creates vesting accounts based on the
// generated data. The startTime of each account increases progressively to ensure some accounts have
// locked balances, catering to the testing requirements.
func (s *VestingModuleTestSuite) initPeriodicVestingAccounts(count int) testnode.GenesisOption {
	initCoinsForGasFee := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(initBalanceForGasFee)))

	names := testfactory.GenerateAccounts(count)
	s.accounts.Store(PeriodicVestingAccountType, accountDispenser{names: names})

	allocationPerPeriod := vestingAmount / 4
	periods := vestingtypes.Periods{
		vestingtypes.Period{Length: int64(6), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, allocationPerPeriod)}},
		vestingtypes.Period{Length: int64(6), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, allocationPerPeriod)}},
		vestingtypes.Period{Length: int64(6), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, allocationPerPeriod)}},
		vestingtypes.Period{Length: int64(6), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, allocationPerPeriod)}},
	}

	gAccounts := authtypes.GenesisAccounts{}
	balances := []banktypes.Balance{}

	startTime := tmtime.Now()
	for i := range names {
		bAccount, defaultBalance := testfactory.NewBaseAccount(s.kr, names[i])
		gAccount, balance, err := genesis.NewGenesisPeriodicVestingAccount(
			bAccount.GetAddress().String(),
			defaultBalance,
			initCoinsForGasFee,
			startTime,
			periods,
		)
		require.NoError(s.T(), err)

		gAccounts = append(gAccounts, gAccount)
		balances = append(balances, balance)

		// the startTime is increased for each account to be able to test various scenarios
		startTime = startTime.Add(5 * time.Second)
	}

	return func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		gs, err := genesis.AddGenesisAccountsWithBalancesToGenesisState(s.ecfg, gs, gAccounts, balances)
		assert.NoError(s.T(), err)
		return gs
	}
}

// initContinuousVestingAccounts function initializes continuous vesting accounts for testing purposes.
// It takes the count of accounts as input and returns a GenesisOption. It generates account names,
// base accounts, and balances. It defines start & end times to creates vesting accounts. The start & endTime
// of each account increases progressively to ensure some accounts have locked balances, catering to the
// testing requirements.
func (s *VestingModuleTestSuite) initContinuousVestingAccounts(count int) testnode.GenesisOption {
	initCoinsForGasFee := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(initBalanceForGasFee)))

	names := testfactory.GenerateAccounts(count)
	s.accounts.Store(ContinuousVestingAccountType, accountDispenser{names: names})

	gAccounts := authtypes.GenesisAccounts{}
	balances := []banktypes.Balance{}
	startTime := tmtime.Now()

	for i := range names {
		endTime := startTime.Add(20 * time.Second)

		bAccount, defaultBalance := testfactory.NewBaseAccount(s.kr, names[i])
		gAccount, balance, err := genesis.NewGenesisContinuousVestingAccount(
			bAccount.GetAddress().String(),
			defaultBalance,
			initCoinsForGasFee,
			startTime,
			endTime,
		)
		require.NoError(s.T(), err)

		gAccounts = append(gAccounts, gAccount)
		balances = append(balances, balance)

		// the startTime is increased for each account to be able to test various scenarios
		startTime = startTime.Add(5 * time.Second)
	}

	return func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		gs, err := genesis.AddGenesisAccountsWithBalancesToGenesisState(s.ecfg, gs, gAccounts, balances)
		assert.NoError(s.T(), err)
		return gs
	}
}

// unusedAccount returns an unused account name of the specified account type
// for the VestingModuleTestSuite. If the account type is not found, it returns
// an error.
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
		address := testfactory.GetAddress(s.cctx.Keyring, name).String()
		resAccBytes, err := testfactory.GetRawAccountInfo(s.cctx.GRPCClient, address)
		if err != nil {
			return vestingtypes.ContinuousVestingAccount{}, "", err
		}

		err = vAcc.Unmarshal(resAccBytes)
		if err != nil {
			return vestingtypes.ContinuousVestingAccount{}, "", err
		}
		if vAcc.StartTime > minStartTime {
			return vAcc, name, nil
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
		address := testfactory.GetAddress(s.cctx.Keyring, name).String()
		resAccBytes, err := testfactory.GetRawAccountInfo(s.cctx.GRPCClient, address)
		if err != nil {
			return vestingtypes.PeriodicVestingAccount{}, "", err
		}

		err = vAcc.Unmarshal(resAccBytes)
		if err != nil {
			return vestingtypes.PeriodicVestingAccount{}, "", err
		}
		if vAcc.StartTime > minStartTime {
			return vAcc, name, nil
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
			return vestingtypes.DelayedVestingAccount{}, "", err
		}

		address := testfactory.GetAddress(s.cctx.Keyring, name).String()
		resAccBytes, err := testfactory.GetRawAccountInfo(s.cctx.GRPCClient, address)
		if err != nil {
			return vestingtypes.DelayedVestingAccount{}, "", err
		}

		err = vAcc.Unmarshal(resAccBytes)
		if err != nil {
			return vestingtypes.DelayedVestingAccount{}, "", err
		}
		if vAcc.EndTime > minEndTime {
			return vAcc, name, nil
		}
	}
}
