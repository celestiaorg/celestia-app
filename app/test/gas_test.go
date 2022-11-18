package app_test

import (
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/testutil/gasmonitor"
	"github.com/celestiaorg/celestia-app/testutil/testnode"
	"github.com/celestiaorg/celestia-app/x/blob"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/testutil/mock"
	sdk "github.com/cosmos/cosmos-sdk/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func TestGasConsumptiontionTestSuite(t *testing.T) {
	// t.Skip("Skipping Gas Consumption Test")
	suite.Run(t, new(GasConsumptionTestSuite))
}

type GasConsumptionTestSuite struct {
	suite.Suite

	cleanups []func()
	accounts []string
	cctx     testnode.Context

	gasMon *gasmonitor.Decorator

	mut            sync.Mutex
	accountCounter int
}

func (s *GasConsumptionTestSuite) SetupSuite() {
	if testing.Short() {
		s.T().Skip("skipping test in unit-tests or race-detector mode.")
	}

	s.T().Log("setting up integration test suite")
	require := s.Require()

	// we create an arbitrary number of funded accounts
	for i := 0; i < 300; i++ {
		s.accounts = append(s.accounts, tmrand.Str(9))
	}

	genState, kr, err := testnode.DefaultGenesisState(s.accounts...)
	require.NoError(err)

	tmCfg := testnode.DefaultTendermintConfig()
	tmCfg.Consensus.TimeoutCommit = time.Second

	s.gasMon = gasmonitor.NewDecorator()

	tmNode, app, cctx, err := testnode.New(
		s.T(),
		testnode.DefaultParams(),
		tmCfg,
		false,
		genState,
		s.gasMon,
		kr,
	)
	require.NoError(err)

	cctx, stopNode, err := testnode.StartNode(tmNode, cctx)
	require.NoError(err)
	s.cleanups = append(s.cleanups, stopNode)

	cctx, cleanupGRPC, err := testnode.StartGRPCServer(app, testnode.DefaultAppConfig(), cctx)
	require.NoError(err)
	s.cleanups = append(s.cleanups, cleanupGRPC)

	s.cctx = cctx
}

func (s *GasConsumptionTestSuite) TearDownSuite() {
	s.T().Log("tearing down integration test suite")
	for _, c := range s.cleanups {
		c()
	}
}

func (s *GasConsumptionTestSuite) unusedAccount() string {
	s.mut.Lock()
	acc := s.accounts[s.accountCounter]
	s.accountCounter++
	s.mut.Unlock()
	return acc
}

func (s *GasConsumptionTestSuite) TestStandardGasConsumption() {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	type gasTest struct {
		name    string
		msgFunc func() (msg sdk.Msg, signer string)
		hash    string
	}
	tests := []gasTest{
		{
			name: "send 1 utia",
			msgFunc: func() (msg sdk.Msg, signer string) {
				account1, account2 := s.unusedAccount(), s.unusedAccount()
				msgSend := banktypes.NewMsgSend(
					getAddress(account1, s.cctx.Keyring),
					getAddress(account2, s.cctx.Keyring),
					sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1))),
				)
				return msgSend, account1
			},
		},
		{
			// demonstrate that a send transactions use different amounts of gas
			// depending on the number of funds
			name: "send 1,000,000 TIA",
			msgFunc: func() (msg sdk.Msg, signer string) {
				account1, account2 := s.unusedAccount(), s.unusedAccount()
				msgSend := banktypes.NewMsgSend(
					getAddress(account1, s.cctx.Keyring),
					getAddress(account2, s.cctx.Keyring),
					sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000000000))),
				)
				return msgSend, account1
			},
		},
		{
			name: "delegate 1 TIA",
			msgFunc: func() (msg sdk.Msg, signer string) {
				valopAddr := sdk.ValAddress(getAddress("validator", s.cctx.Keyring))
				account1 := s.unusedAccount()
				account1Addr := getAddress(account1, s.cctx.Keyring)
				msg = stakingtypes.NewMsgDelegate(account1Addr, valopAddr, sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000)))
				return msg, account1
			},
		},
		{
			// This is running what should be an identical tx from above. Its
			// purpose is to demonstrate that it takes up roughly ~1000 more gas
			// than the first transaction
			name: "delegate 1 TIA 2",
			msgFunc: func() (msg sdk.Msg, signer string) {
				valopAddr := sdk.ValAddress(getAddress("validator", s.cctx.Keyring))
				account1 := s.unusedAccount()
				account1Addr := getAddress(account1, s.cctx.Keyring)
				msg = stakingtypes.NewMsgDelegate(account1Addr, valopAddr, sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000)))
				return msg, account1
			},
		},
		{
			// This tx will fail on purpose. In order for it to pass, we have to
			// create a tx with an account that has already delegated && has no
			// other txs in this block. Even though this tx fails, it is still
			// useful to know how much gas was used.
			name: "failed undelegate 1 TIA",
			msgFunc: func() (msg sdk.Msg, signer string) {
				valopAddr := sdk.ValAddress(getAddress("validator", s.cctx.Keyring))
				account1 := s.unusedAccount()
				account1Addr := getAddress(account1, s.cctx.Keyring)
				msg = stakingtypes.NewMsgUndelegate(account1Addr, valopAddr, sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000)))
				return msg, account1
			},
		},
		{
			name: "undelegate 1 TIA",
			msgFunc: func() (msg sdk.Msg, signer string) {
				valAccAddr := getAddress("validator", s.cctx.Keyring)
				valopAddr := sdk.ValAddress(valAccAddr)
				msg = stakingtypes.NewMsgUndelegate(valAccAddr, valopAddr, sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000)))
				return msg, "validator"
			},
		},
		{
			name: "create validator",
			msgFunc: func() (msg sdk.Msg, signer string) {
				pv := mock.NewPV()
				account := s.unusedAccount()
				valopAccAddr := getAddress(account, s.cctx.Keyring)
				valopAddr := sdk.ValAddress(valopAccAddr)
				evmAddr := common.BigToAddress(big.NewInt(420))
				msg, err := stakingtypes.NewMsgCreateValidator(
					valopAddr,
					pv.PrivKey.PubKey(),
					sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000)),
					stakingtypes.NewDescription("taco tuesday", "my keybase", "www.celestia.org", "ping @celestiaorg on twitter", "fake validator"),
					stakingtypes.NewCommissionRates(sdk.NewDecWithPrec(6, 02), sdk.NewDecWithPrec(12, 02), sdk.NewDecWithPrec(1, 02)),
					sdk.NewInt(1000000),
					valopAccAddr,
					evmAddr,
				)
				require.NoError(s.T(), err)
				return msg, account
			},
		},
		{
			name: "create vesting account",
			msgFunc: func() (msg sdk.Msg, signer string) {
				vestAccName := "vesting"
				_, _, err := s.cctx.Keyring.NewMnemonic(vestAccName, keyring.English, "", "", hd.Secp256k1)
				require.NoError(s.T(), err)
				sendAcc := s.unusedAccount()
				sendingAccAddr := getAddress(sendAcc, s.cctx.Keyring)
				vestAccAddr := getAddress(vestAccName, s.cctx.Keyring)
				msg = vestingtypes.NewMsgCreateVestingAccount(
					sendingAccAddr,
					vestAccAddr,
					sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000))),
					10000, true,
				)
				return msg, sendAcc
			},
		},
	}
	// sign and submit the transactions
	for i, tt := range tests {
		msg, signer := tt.msgFunc()
		res, err := testnode.SignAndBroadcastTx(encCfg, s.cctx, signer, msg)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), res)
		require.Equal(s.T(), abci.CodeTypeOK, res.Code)
		tests[i].hash = res.TxHash
	}

	// send a few PFBs of various sizes (todo: switch to a format similar to the above after we
	// refactor wirePFB and malleation)
	signer := blobtypes.NewKeyringSigner(s.cctx.Keyring, s.unusedAccount(), s.cctx.ChainID)
	res, err := blob.SubmitPayForBlob(
		s.cctx.GoContext(),
		signer,
		s.cctx.GRPCClient,
		[]byte{1, 2, 3, 4, 5, 6, 7, 8},
		tmrand.Bytes(1000),
		1000000000000, // arbitrarily large gas limit
	)
	require.NoError(s.T(), err)
	require.Equal(s.T(), abci.CodeTypeOK, res.Code)
	tests = append(tests, gasTest{hash: res.TxHash, name: "1KB PFB"})

	signer = blobtypes.NewKeyringSigner(s.cctx.Keyring, s.unusedAccount(), s.cctx.ChainID)
	res, err = blob.SubmitPayForBlob(
		s.cctx.GoContext(),
		signer,
		s.cctx.GRPCClient,
		[]byte{9, 10, 11, 12, 13, 14, 15, 16},
		tmrand.Bytes(1000000),
		1000000000000, // arbitrarily large gas limit
	)
	require.NoError(s.T(), err)
	require.Equal(s.T(), abci.CodeTypeOK, res.Code)
	tests = append(tests, gasTest{hash: res.TxHash, name: "1MB PFB"})

	// wait two blocks for txs to clear
	err = s.cctx.WaitForNextBlock()
	require.NoError(s.T(), err)
	err = s.cctx.WaitForNextBlock()
	require.NoError(s.T(), err)

	// // todo: remove these checks
	// for _, tt := range tests {
	// 	res, err := queryTx(s.cctx.Context, tt.hash, false)
	// 	require.NoError(s.T(), err)
	// 	if res.TxResult.Code != abci.CodeTypeOK {
	// 		fmt.Println("failed tx", tt.name, res.TxResult.Code, res.TxResult.Info, res.TxResult.Log)
	// 	}
	// }

	monHashMap := make(map[string]*gasmonitor.MonitoredGasMeter)
	for _, monitor := range s.gasMon.Monitors {
		monHashMap[monitor.Hash] = monitor
	}
	monNameMap := make(map[string]*gasmonitor.MonitoredGasMeter)
	for _, tt := range tests {
		monitor := monHashMap[tt.hash]
		if monitor == nil {
			continue
		}
		monitor.Summarize()
		monitor.Name = tt.name
		monNameMap[tt.name] = monitor
	}
	// we have to manually do this for PFBs because the hashes are different.
	// TODO: fix after we refactor malleation process
	pfb1KB := s.gasMon.Monitors[len(s.gasMon.Monitors)-2]
	pfb1MB := s.gasMon.Monitors[len(s.gasMon.Monitors)-1]
	pfb1KB.Summarize()
	pfb1KB.Name = "1KB PFB"
	monNameMap[pfb1KB.Name] = pfb1KB
	pfb1MB.Summarize()
	pfb1MB.Name = "1MB PFB"
	monNameMap[pfb1MB.Name] = pfb1MB

	err = gasmonitor.SaveJSON("gas", monNameMap)
	require.NoError(s.T(), err)
}

func getAddress(account string, kr keyring.Keyring) sdk.AccAddress {
	rec, err := kr.Key(account)
	if err != nil {
		panic(err)
	}
	addr, err := rec.GetAddress()
	if err != nil {
		panic(err)
	}
	return addr
}

func getPubKey(account string, kr keyring.Keyring) cryptotypes.PubKey {
	rec, err := kr.Key(account)
	if err != nil {
		panic(err)
	}
	pub, err := rec.GetPubKey()
	if err != nil {
		panic(err)
	}
	return pub
}
