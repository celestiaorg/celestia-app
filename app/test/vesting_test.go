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

func TestVestingModule(t *testing.T) {
	suite.Run(t, new(VestingModuleTestSuite))
}

type VestingModuleTestSuite struct {
	suite.Suite

	// Regular accounts
	accounts       []string
	mut            sync.Mutex
	accountCounter int
}

func (s *VestingModuleTestSuite) SetupSuite() {
	t := s.T()
	t.Log("setting up vesting module test suite")
	s.accounts = testfactory.GenerateAccounts(300)
}

func (s *VestingModuleTestSuite) unusedAccount() string {
	s.mut.Lock()
	acc := s.accounts[s.accountCounter]
	s.accountCounter++
	s.mut.Unlock()
	return acc
}

func (s *VestingModuleTestSuite) NewNetworkWithGenesisOpt(kr keyring.Keyring, genesisOpts ...testnode.GenesisOption) (cctx testnode.Context, rpcAddr, grpcAddr string) {
	tmCfg := testnode.DefaultTendermintConfig()
	tmCfg.Consensus.TargetHeightDuration = time.Millisecond * 1
	appConf := testnode.DefaultAppConfig()

	for _, name := range s.accounts {
		testfactory.NewBaseAccount(kr, name)
	}

	cctx, rpcAddr, grpcAddr = testnode.NewNetwork(s.T(), testnode.DefaultParams(), tmCfg, appConf, []string{}, genesisOpts...)
	cctx.Keyring = kr

	return cctx, rpcAddr, grpcAddr
}

func (s *VestingModuleTestSuite) TestGenesisDelayedVestingAccounts() {
	// initial unlocked allocation to pay the gas fees
	initBalanceForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(10))

	tests := []struct {
		name    string
		endTime time.Time
	}{
		{
			name:    "ValidVestingAccount",
			endTime: time.Now().Add(time.Hour * 50),
		},
		{
			name:    "ImmediateVesting",
			endTime: time.Now(),
		},
		{
			name:    "NegativeEndTime",
			endTime: time.Now().Add(-time.Second * 10),
		},
	}

	kr := testfactory.GenerateKeyring()

	gsOpt := func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		vAccs := authtypes.GenesisAccounts{}
		vBals := []banktypes.Balance{}
		for i, tt := range tests {
			tests[i].accName = fmt.Sprintf("vesting_%d", i)
			bacc, coins := testfactory.NewBaseAccount(kr, tests[i].accName)
			acc := vestingtypes.NewDelayedVestingAccount(bacc, coins, tt.endTime.Unix())
			vAccs = append(vAccs, acc)
			vBals = append(vBals, banktypes.Balance{
				Address: acc.GetAddress().String(),
				Coins:   acc.GetOriginalVesting().Add(initBalanceForGasFee),
			})
		}

		var err error
		gs, err = AddAccountsToGenesisState(gs, vAccs...)
		assert.NoError(s.T(), err)

		gs, err = AddBalancesToGenesisState(gs, vBals...)
		assert.NoError(s.T(), err)

		return gs
	}

	cctx, _, _ := s.NewNetworkWithGenesisOpt(kr, gsOpt)

	assert.NoError(s.T(), cctx.WaitForNextBlock())

	for _, tt := range tests {
		s.Run(tt.name, func() {
			accAddress := getAddress(tt.accName, cctx.Keyring).String()

			// Test account details correctness
			cli := authtypes.NewQueryClient(cctx.GRPCClient)
			res, err := cli.Account(context.Background(), &authtypes.QueryAccountRequest{
				Address: accAddress,
			})
			assert.NoError(s.T(), err)

			var qAcc vestingtypes.DelayedVestingAccount
			err = qAcc.Unmarshal(res.Account.Value)
			assert.NoError(s.T(), err)

			// Checking the queried account data
			assert.Equal(s.T(), accAddress, qAcc.GetAddress().String(), "account address must match")
			assert.EqualValues(s.T(), tt.endTime.Unix(), qAcc.GetEndTime(), "end time must match")
			assert.EqualValues(s.T(),
				testfactory.BaseAccountDefaultBalance,
				qAcc.GetOriginalVesting().AmountOf(app.BondDenom).Int64(),
				"original vesting amount must match")


			// Test the locking mechanism
			// If the end time is already passed, the funds must be unlocked
			// and we should be able to transfer some of it to another account
			mustSucceed := tt.endTime.Before(time.Now())

			assert.NoError(s.T(), cctx.WaitForNextBlock())


			balances, err := GetAccountSpendableBalance(cctx.GRPCClient, accAddress)
			assert.NoError(s.T(), err)

			expectedSpendableBal := int64(0)
			if mustSucceed {
				expectedSpendableBal = testfactory.BaseAccountDefaultBalance
			}
			expectedSpendableBal += initBalanceForGasFee.Amount.Int64()
			assert.EqualValues(s.T(),
				expectedSpendableBal,
				balances.AmountOf(app.BondDenom).Int64(),
				"spendable balance must match")


			s.testTransferVestingAmount(cctx, tt.accName, mustSucceed)

			balancesAfter, err := GetAccountSpendableBalance(cctx.GRPCClient, accAddress)
			assert.NoError(s.T(), err)

			expectedSpendableBalAfterTx := expectedSpendableBal - 1 // -1utia for gas fee of the tx above
			if mustSucceed {
				// if the transfer was successful
				expectedSpendableBalAfterTx -= testfactory.BaseAccountDefaultBalance
			}

			assert.EqualValues(s.T(),
				expectedSpendableBalAfterTx,
				balancesAfter.AmountOf(app.BondDenom).Int64(),
				"spendable balance must be equal")


			// test delegation of only the locked account(s)
			if !mustSucceed {
				s.testDelegation(cctx, tt.accName)
				s.testClaimDelegationReward(cctx, tt.accName)
			}
		})
	}
}

func (s *VestingModuleTestSuite) TestGenesisPeriodicVestingAccounts() {
	// initial unlocked allocation to pay the gas fees
	initBalanceForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(10))

	startTime := tmtime.Now()
	periods := vestingtypes.Periods{
		vestingtypes.Period{Length: int64(10), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, 2500)}},
		vestingtypes.Period{Length: int64(8), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, 2500)}},
		vestingtypes.Period{Length: int64(8), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, 2500)}},
		vestingtypes.Period{Length: int64(10), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, 2500)}},
	}

	const accName = "period_vesting_0"
	kr := testfactory.GenerateKeyring()
	bacc, coins := testfactory.NewBaseAccount(kr, accName)
	pva := vestingtypes.NewPeriodicVestingAccount(bacc, coins, startTime.Unix(), periods)

	gsOpt := func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		vAccs := authtypes.GenesisAccounts{pva}
		vBals := []banktypes.Balance{
			{
				Address: pva.GetAddress().String(),
				Coins:   pva.GetOriginalVesting().Add(initBalanceForGasFee),
			},
		}

		var err error
		gs, err = AddAccountsToGenesisState(gs, vAccs...)
		assert.NoError(s.T(), err)

		gs, err = AddBalancesToGenesisState(gs, vBals...)
		assert.NoError(s.T(), err)

		return gs
	}

	cctx, _, _ := s.NewNetworkWithGenesisOpt(kr, gsOpt)

	assert.NoError(s.T(), cctx.WaitForNextBlock())

	accAddress := getAddress(accName, cctx.Keyring).String()

	// Test account details correctness
	cli := authtypes.NewQueryClient(cctx.GRPCClient)
	res, err := cli.Account(context.Background(), &authtypes.QueryAccountRequest{
		Address: accAddress,
	})
	assert.NoError(s.T(), err)

	var qAcc vestingtypes.PeriodicVestingAccount
	err = qAcc.Unmarshal(res.Account.Value)
	assert.NoError(s.T(), err)

	// Checking the queried account data
	assert.Equal(s.T(), accAddress, qAcc.GetAddress().String(), "account address must match")
	assert.EqualValues(s.T(), startTime.Unix(), qAcc.GetStartTime(), "start time must match")
	assert.EqualValues(s.T(),
		testfactory.BaseAccountDefaultBalance,
		qAcc.GetOriginalVesting().AmountOf(app.BondDenom).Int64(),
		"original vesting amount must match")
	assert.EqualValues(s.T(), periods, qAcc.VestingPeriods, "periods must match")


	// Test available balance
	for i := 0; i < 2; i++ { // We just let one period to be unlocked to keep some locked amount to test other stuff

		currentLen := time.Since(startTime).Seconds()

		balances, err := GetAccountSpendableBalance(cctx.GRPCClient, accAddress)
		assert.NoError(s.T(), err)

		expectedSpendableBal := initBalanceForGasFee.Amount.Int64()
		passedLen := float64(0)
		for _, pr := range periods {
			passedLen += float64(pr.Length)
			if currentLen > passedLen {
				expectedSpendableBal += pr.GetAmount()[0].Amount.Int64()
			}
		}

		assert.EqualValues(s.T(),
			expectedSpendableBal,
			balances.AmountOf(app.BondDenom).Int64(),
			"spendable balance must match")

		_, err = cctx.WaitForTimestamp(startTime.Add(periods[i].Duration() + 10*time.Millisecond)) // Wait for the next period to be passed
		assert.NoError(s.T(), err)
	}

	s.testTransferVestingAmount(cctx, accName, false)
	s.testDelegation(cctx, accName)
	s.testClaimDelegationReward(cctx, accName)
}

func (s *VestingModuleTestSuite) TestGenesisContinuesVestingAccounts() {
	// initial unlocked allocation to pay the gas fees
	initBalanceForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(10))

	startTime := tmtime.Now().Add(10 * time.Second)
	endTime := startTime.Add(20 * time.Second)

	const accName = "cont_vesting_0"
	kr := testfactory.GenerateKeyring()

	bacc, coins := testfactory.NewBaseAccount(kr, accName)
	cva := vestingtypes.NewContinuousVestingAccount(bacc, coins, startTime.Unix(), endTime.Unix())

	gsOpt := func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		vAccs := authtypes.GenesisAccounts{cva}
		vBals := []banktypes.Balance{
			{
				Address: cva.GetAddress().String(),
				Coins:   cva.GetOriginalVesting().Add(initBalanceForGasFee),
			},
		}

		var err error
		gs, err = AddAccountsToGenesisState(gs, vAccs...)
		assert.NoError(s.T(), err)

		gs, err = AddBalancesToGenesisState(gs, vBals...)
		assert.NoError(s.T(), err)

		return gs
	}

	cctx, _, _ := s.NewNetworkWithGenesisOpt(kr, gsOpt)

	assert.NoError(s.T(), cctx.WaitForNextBlock())

	accAddress := getAddress(accName, cctx.Keyring).String()

	// Test account details correctness
	acli := authtypes.NewQueryClient(cctx.GRPCClient)
	res, err := acli.Account(context.Background(), &authtypes.QueryAccountRequest{
		Address: accAddress,
	})
	assert.NoError(s.T(), err)

	var qAcc vestingtypes.ContinuousVestingAccount
	err = qAcc.Unmarshal(res.Account.Value)
	assert.NoError(s.T(), err)

	// Checking the queried account data
	assert.Equal(s.T(), accAddress, qAcc.GetAddress().String(), "account address must match")
	assert.EqualValues(s.T(), startTime.Unix(), qAcc.GetStartTime(), "start time must match")
	assert.EqualValues(s.T(), endTime.Unix(), qAcc.GetEndTime(), "end time must match")
	assert.EqualValues(s.T(),
		testfactory.BaseAccountDefaultBalance,
		qAcc.GetOriginalVesting().AmountOf(app.BondDenom).Int64(),
		"original vesting must match")

	/*--------*/

	// Test available balance
	for i := 0; i < 5; i++ {
		queryTime := tmtime.Now()

		balances, err := GetAccountSpendableBalance(cctx.GRPCClient, accAddress)
		assert.NoError(s.T(), err)

		minExpectedSpendableBal := cva.GetVestedCoins(queryTime).Add(initBalanceForGasFee)

		assert.LessOrEqual(s.T(),
			balances.AmountOf(app.BondDenom).Int64(),
			minExpectedSpendableBal.AmountOf(app.BondDenom).Int64(),
		)

		_, err = cctx.WaitForTimestamp(startTime.Add(5)) // Wait for a while
		assert.NoError(s.T(), err)
	}
	/*--------*/

	s.testTransferVestingAmount(cctx, accName, false)
	s.testDelegation(cctx, accName)
	s.testClaimDelegationReward(cctx, accName)
}

func (s *VestingModuleTestSuite) testTransferVestingAmount(cctx testnode.Context, accName string, mustSucceed bool) {
	randomAcc := s.unusedAccount()
	msgSend := banktypes.NewMsgSend(
		getAddress(accName, cctx.Keyring),
		getAddress(randomAcc, cctx.Keyring),
		sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(testfactory.BaseAccountDefaultBalance))), // try to transfer the locked amount
	)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	resTx, err := testnode.SignAndBroadcastTx(encCfg, cctx.Context, accName, []sdk.Msg{msgSend}...)
	assert.NoError(s.T(), err)

	resQ, err := cctx.WaitForTx(resTx.TxHash, 10)
	assert.NoError(s.T(), err)

	if mustSucceed {
		assert.EqualValues(s.T(), 0, resQ.TxResult.Code, "the transfer TX must succeed")
	} else {
		assert.EqualValues(s.T(), 5, resQ.TxResult.Code, "the transfer TX must fail")
	}
}

func (s *VestingModuleTestSuite) testDelegation(cctx testnode.Context, accName string) {
	accAddress := getAddress(accName, cctx.Keyring).String()

	del, err := GetAccountDelegations(cctx.GRPCClient, accAddress)
	assert.NoError(s.T(), err)
	assert.Empty(s.T(), del, "initial delegation must be empty")

	validators, err := GetValidators(cctx.GRPCClient)
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), validators)

	msgDelg := stakingtypes.NewMsgDelegate(
		getAddress(accName, cctx.Keyring),
		validators[0].GetOperator(),
		sdk.NewCoin(app.BondDenom, sdk.NewInt(testfactory.BaseAccountDefaultBalance)),
	)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	resTx, err := testnode.SignAndBroadcastTx(encCfg, cctx.Context, accName, []sdk.Msg{msgDelg}...)
	assert.NoError(s.T(), err)

	resQ, err := cctx.WaitForTx(resTx.TxHash, 10)
	assert.NoError(s.T(), err)
	assert.EqualValues(s.T(), 0, resQ.TxResult.Code, "the delegation TX must succeed")

	// verify the delegations
	del, err = GetAccountDelegations(cctx.GRPCClient, accAddress)
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), del, "delegations must not be empty")
	assert.EqualValues(s.T(),
		testfactory.BaseAccountDefaultBalance,
		del[0].Balance.Amount.Int64(),
		"delegation amount must match")
}

func (s *VestingModuleTestSuite) testClaimDelegationReward(cctx testnode.Context, accName string) {
	assert.NoError(s.T(), cctx.WaitForNextBlock())

	accAddress := getAddress(accName, cctx.Keyring).String()

	cli := distributiontypes.NewQueryClient(cctx.GRPCClient)
	resR, err := cli.DelegationTotalRewards(
		context.Background(),
		&distributiontypes.QueryDelegationTotalRewardsRequest{
			DelegatorAddress: accAddress,
		},
	)
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), resR, "empty staking rewards")
	assert.NotEmpty(s.T(), resR.Rewards)

	rewardAmount := resR.Rewards[0].Reward.AmountOf(app.BondDenom).RoundInt().Int64()
	assert.Greater(s.T(), rewardAmount, int64(0), "rewards must be more than zero")

	balancesBefore, err := GetAccountSpendableBalance(cctx.GRPCClient, accAddress)
	assert.NoError(s.T(), err)

	// minExpectedBalance is used because more tokens may be vested to the
	// account in the middle of this test
	minExpectedBalance := balancesBefore.AmountOf(app.BondDenom).Int64() + rewardAmount

	validators, err := GetValidators(cctx.GRPCClient)
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), validators, "empty validators set")

	msg := distributiontypes.NewMsgWithdrawDelegatorReward(
		getAddress(accName, cctx.Keyring),
		validators[0].GetOperator(),
	)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	resTx, err := testnode.SignAndBroadcastTx(encCfg, cctx.Context, accName, []sdk.Msg{msg}...)
	assert.NoError(s.T(), err)

	resQ, err := cctx.WaitForTx(resTx.TxHash, 10)
	assert.NoError(s.T(), err)
	assert.EqualValues(s.T(), 0, resQ.TxResult.Code, "the claim reward TX must succeed")

	// Check if the reward amount in the account
	balancesAfter, err := GetAccountSpendableBalance(cctx.GRPCClient, accAddress)
	assert.NoError(s.T(), err)

	assert.GreaterOrEqual(s.T(), balancesAfter.AmountOf(app.BondDenom).Int64(), minExpectedBalance, "Minimum balance after claiming reward")
}

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

func GetValidators(grpcConn *grpc.ClientConn) (stakingtypes.Validators, error) {
	scli := stakingtypes.NewQueryClient(grpcConn)
	vres, err := scli.Validators(context.Background(), &stakingtypes.QueryValidatorsRequest{})

	if vres == nil {
		return stakingtypes.Validators{}, err
	}
	return vres.Validators, err
}

func GetAccountDelegations(grpcConn *grpc.ClientConn, address string) (stakingtypes.DelegationResponses, error) {
	cli := stakingtypes.NewQueryClient(grpcConn)
	res, err := cli.DelegatorDelegations(context.Background(),
		&stakingtypes.QueryDelegatorDelegationsRequest{DelegatorAddr: address})
	if err != nil {
		return nil, err
	}

	return res.DelegationResponses, err
}

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
