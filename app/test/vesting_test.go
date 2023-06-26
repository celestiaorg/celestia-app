package app_test

import (
	"context"
	"crypto/tls"
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
	"github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	tmtime "github.com/tendermint/tendermint/types/time"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
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
		accName string // will be filled by the code
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
			acc := s.newDelayedVestingAccount(kr, tests[i].accName, tt.endTime)
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

	cctx, _, grpcAddr := s.NewNetworkWithGenesisOpt(kr, gsOpt)

	assert.NoError(s.T(), cctx.WaitForNextBlock())

	grpcConn, err := GrpcConnect(grpcAddr, false)
	assert.NoError(s.T(), err)

	for _, tt := range tests {
		s.Run(tt.name, func() {
			accAddress := getAddress(tt.accName, cctx.Keyring).String()

			// Test account details correctness
			acli := authtypes.NewQueryClient(grpcConn)
			res, err := acli.Account(context.Background(), &authtypes.QueryAccountRequest{
				Address: accAddress,
			})
			assert.NoError(s.T(), err)

			var qAcc vestingtypes.DelayedVestingAccount
			err = qAcc.Unmarshal(res.Account.Value)
			assert.NoError(s.T(), err)

			// Checking the queried account data
			assert.Equal(s.T(), accAddress, qAcc.GetAddress().String(), "account address must match")
			assert.EqualValues(s.T(), tt.endTime.Unix(), qAcc.GetEndTime(), "end time must match")

			bondDenom := qAcc.GetOriginalVesting().GetDenomByIndex(0)
			assert.EqualValues(s.T(),
				testfactory.BaseAccountDefaultBalance,
				qAcc.GetOriginalVesting().AmountOf(bondDenom).Int64(),
				"original vesting must match")

			/*--------*/

			// Test the locking mechanism
			// If the end time is already passed, the funds must be unlocked
			// and we should be able to transfer some of it to another account
			mustSuceed := tt.endTime.Before(time.Now())

			assert.NoError(s.T(), cctx.WaitForNextBlock())

			/*--------*/

			// Test available balance
			bcli := banktypes.NewQueryClient(grpcConn)
			bres, err := bcli.SpendableBalances(context.Background(), &banktypes.QuerySpendableBalancesRequest{
				Address: accAddress,
			})
			assert.NoError(s.T(), err)

			expectedSpendableBal := int64(0)
			if mustSuceed {
				expectedSpendableBal = testfactory.BaseAccountDefaultBalance
			}
			expectedSpendableBal += initBalanceForGasFee.Amount.Int64()
			assert.EqualValues(s.T(),
				expectedSpendableBal,
				bres.GetBalances().AmountOf(bondDenom).Int64(),
				"spendable balance must match")

			/*--------*/

			// Test transfer ability
			randomAcc := s.unusedAccount()
			msgSend := banktypes.NewMsgSend(
				getAddress(tt.accName, cctx.Keyring),
				getAddress(randomAcc, cctx.Keyring),
				sdk.NewCoins(sdk.NewCoin(bondDenom, sdk.NewInt(1+initBalanceForGasFee.Amount.Int64()))),
			)
			encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
			resTx, err := testnode.SignAndBroadcastTx(encCfg, cctx.Context, tt.accName, []sdk.Msg{msgSend}...)
			assert.NoError(s.T(), err)

			resQ, err := cctx.WaitForTx(resTx.TxHash, 10)
			assert.NoError(s.T(), err)

			if mustSuceed {
				assert.EqualValues(s.T(), 0, resQ.TxResult.Code, "the transfer TX must succeed")
			} else {
				assert.EqualValues(s.T(), 5, resQ.TxResult.Code, "the transfer TX must fail")
			}

			/*--------*/

			// Test available balance after transfer
			bres, err = bcli.SpendableBalances(context.Background(), &banktypes.QuerySpendableBalancesRequest{
				Address: accAddress,
			})
			assert.NoError(s.T(), err)

			expectedSpendableBalAfterTx := expectedSpendableBal - 1 // -1utia for gas fee of the tx above
			if mustSuceed {
				// if the transfer was successful
				expectedSpendableBalAfterTx -= 1 + initBalanceForGasFee.Amount.Int64()
			}

			assert.EqualValues(s.T(), // we pay for gas as well
				bres.GetBalances().AmountOf(bondDenom).Int64(),
				expectedSpendableBalAfterTx,
				"spendable balance must be equal")

			/*--------*/

			del, err := GetAccountDelegations(grpcConn, accAddress)
			assert.NoError(s.T(), err)
			assert.Empty(s.T(), del, "initial delegation must be empty")

			validators, err := GetValidators(grpcConn)
			assert.NoError(s.T(), err)
			assert.NotEmpty(s.T(), validators)

			msgDelg := stakingtypes.NewMsgDelegate(
				getAddress(tt.accName, cctx.Keyring),
				validators[0].GetOperator(),
				sdk.NewCoin(bondDenom, sdk.NewInt(1+initBalanceForGasFee.Amount.Int64())),
			)
			// encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
			resTx, err = testnode.SignAndBroadcastTx(encCfg, cctx.Context, tt.accName, []sdk.Msg{msgDelg}...)
			assert.NoError(s.T(), err)

			resQ, err = cctx.WaitForTx(resTx.TxHash, 10)
			assert.NoError(s.T(), err)
			assert.EqualValues(s.T(), 0, resQ.TxResult.Code, "the delegation TX must succeed")

			// verify the delegations
			del, err = GetAccountDelegations(grpcConn, accAddress)
			assert.NoError(s.T(), err)
			assert.NotEmpty(s.T(), del, "delegations must not be empty")
			assert.EqualValues(s.T(),
				1+initBalanceForGasFee.Amount.Int64(),
				del[0].Balance.Amount.Int64(),
				"delegation amount must match")
		})
	}
}

func (s *VestingModuleTestSuite) TestGenesisPeriodicVestingAccounts() {
	// initial unlocked allocation to pay the gas fees
	initBalanceForGasFee := sdk.NewCoin(app.BondDenom, sdk.NewInt(10))

	startTime := tmtime.Now()
	endTime := startTime.Add(24 * time.Second)
	_ = endTime
	periods := vestingtypes.Periods{
		types.Period{Length: int64(10), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, 2500)}},
		types.Period{Length: int64(4), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, 2500)}},
		types.Period{Length: int64(4), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, 2500)}},
		types.Period{Length: int64(6), Amount: sdk.Coins{sdk.NewInt64Coin(app.BondDenom, 2500)}},
	}

	accName := "period_vesting_0"
	kr := testfactory.GenerateKeyring()

	gsOpt := func(gs map[string]json.RawMessage) map[string]json.RawMessage {
		bacc, coins := testfactory.NewBaseAccount(kr, accName)
		pva := vestingtypes.NewPeriodicVestingAccount(bacc, coins, startTime.Unix(), periods)

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

	cctx, _, grpcAddr := s.NewNetworkWithGenesisOpt(kr, gsOpt)

	assert.NoError(s.T(), cctx.WaitForNextBlock())

	grpcConn, err := GrpcConnect(grpcAddr, false)
	assert.NoError(s.T(), err)

	accAddress := getAddress(accName, cctx.Keyring).String()

	// Test account details correctness
	acli := authtypes.NewQueryClient(grpcConn)
	res, err := acli.Account(context.Background(), &authtypes.QueryAccountRequest{
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
		"original vesting must match")
	assert.EqualValues(s.T(), periods, qAcc.VestingPeriods, "periods must match")

	/*--------*/

	// Test available balance
	for i := 0; i < 2; i++ { // We just let one period to be unlocked to keep some locked amount to test other stuff

		bcli := banktypes.NewQueryClient(grpcConn)
		currentLen := time.Since(startTime).Seconds()
		bres, err := bcli.SpendableBalances(context.Background(), &banktypes.QuerySpendableBalancesRequest{
			Address: accAddress,
		})
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
			bres.GetBalances().AmountOf(app.BondDenom).Int64(),
			"spendable balance must match")

		_, err = cctx.WaitForTimestamp(startTime.Add(periods[i].Duration() + 1)) // Wait for the next period to be passed
		assert.NoError(s.T(), err)
	}
	/*--------*/

	// Test transfer ability
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

	assert.EqualValues(s.T(), 5, resQ.TxResult.Code, "the transfer TX must fail")

	/*--------*/

	del, err := GetAccountDelegations(grpcConn, accAddress)
	assert.NoError(s.T(), err)
	assert.Empty(s.T(), del, "initial delegation must be empty")

	validators, err := GetValidators(grpcConn)
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), validators)

	msgDelg := stakingtypes.NewMsgDelegate(
		getAddress(accName, cctx.Keyring),
		validators[0].GetOperator(),
		sdk.NewCoin(app.BondDenom, sdk.NewInt(testfactory.BaseAccountDefaultBalance)),
	)
	resTx, err = testnode.SignAndBroadcastTx(encCfg, cctx.Context, accName, []sdk.Msg{msgDelg}...)
	assert.NoError(s.T(), err)

	resQ, err = cctx.WaitForTx(resTx.TxHash, 10)
	assert.NoError(s.T(), err)
	assert.EqualValues(s.T(), 0, resQ.TxResult.Code, "the delegation TX must succeed")

	// verify the delegations
	del, err = GetAccountDelegations(grpcConn, accAddress)
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), del, "delegations must not be empty")
	assert.EqualValues(s.T(),
		testfactory.BaseAccountDefaultBalance,
		del[0].Balance.Amount.Int64(),
		"delegation amount must match")
}

func (s *VestingModuleTestSuite) newDelayedVestingAccount(kr keyring.Keyring, name string, endTime time.Time) *vestingtypes.DelayedVestingAccount {
	bacc, coins := testfactory.NewBaseAccount(kr, name)
	return vestingtypes.NewDelayedVestingAccount(bacc, coins, endTime.Unix())
}

func GrpcConnect(serverAddr string, tlsEnabled bool) (*grpc.ClientConn, error) {
	if tlsEnabled {
		creds := credentials.NewTLS(&tls.Config{})
		return grpc.Dial(serverAddr, grpc.WithTransportCredentials(creds))
	}
	return grpc.Dial(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
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
