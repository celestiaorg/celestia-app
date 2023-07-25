package gasmonitor_test

import (
	"context"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/celestiaorg/celestia-app/tools/gasmonitor"
	"github.com/celestiaorg/celestia-app/x/blob"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	oldgov "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func TestGasConsumptionTestSuite(t *testing.T) {
	t.Skip("Skipping gas consumption trace.")
	suite.Run(t, new(GasConsumptionTestSuite))
}

type GasConsumptionTestSuite struct {
	suite.Suite

	accounts []string
	cctx     testnode.Context
	ecfg     encoding.Config

	gasMonitor *gasmonitor.Decorator

	mut            sync.Mutex
	accountCounter int
}

func (s *GasConsumptionTestSuite) SetupSuite() {
	t := s.T()
	t.Log("setting up integration test suite")

	accounts := make([]string, 100)
	for i := 0; i < len(accounts); i++ {
		accounts[i] = tmrand.Str(9)
	}

	cfg := testnode.DefaultConfig().WithAccounts(accounts)

	// set the gas monitor
	dec := gasmonitor.NewDecorator()
	s.gasMonitor = dec
	cfg.AppOptions.Set(gasmonitor.AppOptionsKey, dec)

	cctx, _, _ := testnode.NewNetwork(t, cfg)
	s.accounts = cfg.Accounts
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	s.cctx = cctx

	t.Cleanup(func() {
		// all gas traces will be saved to a json file in the directory where
		// the test was ran.
		err := dec.SaveJSON()
		require.NoError(t, err)
	})
}

func (s *GasConsumptionTestSuite) unusedAccount() string {
	s.mut.Lock()
	acc := s.accounts[s.accountCounter]
	s.accountCounter++
	s.mut.Unlock()
	return acc
}

func (s *GasConsumptionTestSuite) TestStandardSDK() {
	t := s.T()
	type test struct {
		name         string
		msgFunc      func() (msgs []sdk.Msg, signer string)
		hash         string
		expectedCode uint32
	}
	tests := []test{
		{
			name: "send 1 utia",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				account1, account2 := s.unusedAccount(), s.unusedAccount()
				msgSend := banktypes.NewMsgSend(
					getAddress(account1, s.cctx.Keyring),
					getAddress(account2, s.cctx.Keyring),
					sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1))),
				)
				return []sdk.Msg{msgSend}, account1
			},
			expectedCode: abci.CodeTypeOK,
		},
		{
			name: "send 1,000,000 TIA",
			msgFunc: func() (msg []sdk.Msg, signer string) {
				account1, account2 := s.unusedAccount(), s.unusedAccount()
				msgSend := banktypes.NewMsgSend(
					getAddress(account1, s.cctx.Keyring),
					getAddress(account2, s.cctx.Keyring),
					sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000000000))),
				)
				return []sdk.Msg{msgSend}, account1
			},
			expectedCode: abci.CodeTypeOK,
		},
		{
			name: "delegate 1 TIA",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				valopAddr := sdk.ValAddress(getAddress("validator", s.cctx.Keyring))
				account1 := s.unusedAccount()
				account1Addr := getAddress(account1, s.cctx.Keyring)
				msg := stakingtypes.NewMsgDelegate(account1Addr, valopAddr, sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000)))
				return []sdk.Msg{msg}, account1
			},
			expectedCode: abci.CodeTypeOK,
		},
		{
			name: "undelegate 1 TIA",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				valAccAddr := getAddress("validator", s.cctx.Keyring)
				valopAddr := sdk.ValAddress(valAccAddr)
				msg := stakingtypes.NewMsgUndelegate(valAccAddr, valopAddr, sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000)))
				return []sdk.Msg{msg}, "validator"
			},
			expectedCode: abci.CodeTypeOK,
		},
		{
			name: "create legacy text governance proposal",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				account := s.unusedAccount()
				content, ok := oldgov.ContentFromProposalType("title", "description", "text")
				require.True(t, ok)
				addr := getAddress(account, s.cctx.Keyring)
				msg, err := oldgov.NewMsgSubmitProposal(
					content,
					sdk.NewCoins(
						sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000000))),
					addr,
				)
				require.NoError(t, err)
				return []sdk.Msg{msg}, account
			},
			// plain text proposals have been removed, so we expect an error. "No
			// handler exists for proposal type"
			expectedCode: govtypes.ErrNoProposalHandlerExists.ABCICode(),
		},
		{
			name: "multiple send sdk.Msgs in one sdk.Tx",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				account1, account2 := s.unusedAccount(), s.unusedAccount()
				msgSend1 := banktypes.NewMsgSend(
					getAddress(account1, s.cctx.Keyring),
					getAddress(account2, s.cctx.Keyring),
					sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1))),
				)
				account3 := s.unusedAccount()
				msgSend2 := banktypes.NewMsgSend(
					getAddress(account1, s.cctx.Keyring),
					getAddress(account3, s.cctx.Keyring),
					sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1))),
				)
				return []sdk.Msg{msgSend1, msgSend2}, account1
			},
			expectedCode: abci.CodeTypeOK,
		},
	}

	// sign and submit the transactions
	for i, tt := range tests {
		msgs, signer := tt.msgFunc()
		res, err := testnode.SignAndBroadcastTx(s.ecfg, s.cctx.Context, signer, msgs...)
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, abci.CodeTypeOK, res.Code, tt.name)
		tests[i].hash = res.TxHash
	}

	require.NoError(s.T(), s.cctx.WaitForNextBlock())

	for _, tt := range tests {
		res, err := testnode.QueryTx(s.cctx.Context, tt.hash, true)
		assert.NoError(t, err)
		assert.Equal(t, tt.expectedCode, res.TxResult.Code, tt.name)
	}

	// run the PFBs after these tests have ran to increment the sequence numbers
	// for the above accounts
	s.submitPayForBlob()
}

func (s *GasConsumptionTestSuite) submitPayForBlob() {
	t := s.T()

	type test struct {
		name  string
		blobs []*blobtypes.Blob
		opts  []blobtypes.TxBuilderOption
	}

	tests := []test{
		{
			"small random typical",
			blobfactory.ManyRandBlobs(t, tmrand.NewRand(), 1),
			[]blobtypes.TxBuilderOption{
				blobtypes.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(9000)))),
				blobtypes.SetGasLimit(90_000),
			},
		},
		{
			"small random typical",
			blobfactory.ManyRandBlobs(t, tmrand.NewRand(), 1, 1),
			[]blobtypes.TxBuilderOption{
				blobtypes.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(9000)))),
				blobtypes.SetGasLimit(90_000),
			},
		},
		{
			"small random with memo",
			blobfactory.ManyRandBlobs(t, tmrand.NewRand(), 1),
			[]blobtypes.TxBuilderOption{
				blobtypes.SetMemo("lol I could stick the rollup block here if I wanted to"),
				blobtypes.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(9000)))),
				blobtypes.SetGasLimit(90_000),
			},
		},
		{
			"large random typical",
			blobfactory.ManyRandBlobs(t, tmrand.NewRand(), 100_000),
			[]blobtypes.TxBuilderOption{
				blobtypes.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(100000)))),
				blobtypes.SetGasLimit(1_000_000),
			},
		},
	}
	count := 0
	for _, tc := range tests {
		s.Run(tc.name, func() {
			signer := blobtypes.NewKeyringSigner(s.cctx.Keyring, s.accounts[count], s.cctx.ChainID)
			res, err := blob.SubmitPayForBlob(context.TODO(), signer, s.cctx.GRPCClient, tc.blobs, tc.opts...)
			require.NoError(t, err)
			require.NotNil(t, res)
			require.Equal(t, abci.CodeTypeOK, res.Code, res.Codespace, res.Logs)
		})
		count++
	}
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
