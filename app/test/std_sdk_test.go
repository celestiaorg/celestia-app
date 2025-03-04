package app_test

import (
	"sync"
	"testing"
	"time"

	"cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/testutil/mock"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	disttypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/celestiaorg/go-square/v2/share"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/random"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	"github.com/celestiaorg/celestia-app/v4/x/minfee"
	signal "github.com/celestiaorg/celestia-app/v4/x/signal/types"
)

func TestStandardSDKIntegrationTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SDK integration test in short mode.")
	}
	suite.Run(t, new(StandardSDKIntegrationTestSuite))
}

type StandardSDKIntegrationTestSuite struct {
	suite.Suite

	accounts []string
	cfg      *testnode.Config
	cctx     testnode.Context
	ecfg     encoding.Config

	mut            sync.Mutex
	accountCounter int
}

func (s *StandardSDKIntegrationTestSuite) SetupSuite() {
	t := s.T()
	t.Log("setting up integration test suite")

	accounts := make([]string, 35)
	for i := 0; i < len(accounts); i++ {
		accounts[i] = random.Str(9)
	}

	s.cfg = testnode.DefaultConfig().WithFundedAccounts(accounts...)
	s.cctx, _, _ = testnode.NewNetwork(t, s.cfg)
	s.accounts = accounts
	s.ecfg = encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
}

func (s *StandardSDKIntegrationTestSuite) unusedAccount() string {
	s.mut.Lock()
	acc := s.accounts[s.accountCounter]
	s.accountCounter++
	s.mut.Unlock()
	return acc
}

func (s *StandardSDKIntegrationTestSuite) getValidatorName() string {
	return s.cfg.Genesis.Validators()[0].Name
}

func (s *StandardSDKIntegrationTestSuite) getValidatorAccount() sdk.ValAddress {
	record, err := s.cfg.Genesis.Keyring().Key(s.getValidatorName())
	s.Require().NoError(err)
	address, err := record.GetAddress()
	s.Require().NoError(err)
	return sdk.ValAddress(address)
}

func (s *StandardSDKIntegrationTestSuite) TestStandardSDK() {
	t := s.T()
	type test struct {
		name         string
		msgFunc      func() (msgs []sdk.Msg, signer string)
		expectedCode uint32
	}
	tests := []test{
		{
			name: "send 1 utia",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				account1, account2 := s.unusedAccount(), s.unusedAccount()
				msgSend := banktypes.NewMsgSend(
					testfactory.GetAddress(s.cctx.Keyring, account1),
					testfactory.GetAddress(s.cctx.Keyring, account2),
					sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewInt(1))),
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
					testfactory.GetAddress(s.cctx.Keyring, account1),
					testfactory.GetAddress(s.cctx.Keyring, account2),
					sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewInt(1000000000000))),
				)
				return []sdk.Msg{msgSend}, account1
			},
			expectedCode: abci.CodeTypeOK,
		},
		{
			name: "delegate 1 TIA",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				valopAddr := sdk.ValAddress(testfactory.GetAddress(s.cctx.Keyring, testnode.DefaultValidatorAccountName))
				account1 := s.unusedAccount()
				account1Addr := testfactory.GetAddress(s.cctx.Keyring, account1)
				msg := stakingtypes.NewMsgDelegate(account1Addr.String(), valopAddr.String(), sdk.NewCoin(app.BondDenom, math.NewInt(1000000)))
				return []sdk.Msg{msg}, account1
			},
			expectedCode: abci.CodeTypeOK,
		},
		{
			name: "undelegate 1 TIA",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				valAccAddr := testfactory.GetAddress(s.cctx.Keyring, testnode.DefaultValidatorAccountName)
				valopAddr := sdk.ValAddress(valAccAddr)
				msg := stakingtypes.NewMsgUndelegate(valAccAddr.String(), valopAddr.String(), sdk.NewCoin(app.BondDenom, math.NewInt(1000000)))
				return []sdk.Msg{msg}, testnode.DefaultValidatorAccountName
			},
			expectedCode: abci.CodeTypeOK,
		},
		{
			name: "create validator",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				pv := mock.NewPV()
				account := s.unusedAccount()
				valopAccAddr := testfactory.GetAddress(s.cctx.Keyring, account)
				valopAddr := sdk.ValAddress(valopAccAddr)
				msg, err := stakingtypes.NewMsgCreateValidator(
					valopAddr.String(),
					pv.PrivKey.PubKey(),
					sdk.NewCoin(app.BondDenom, math.NewInt(1)),
					stakingtypes.NewDescription("taco tuesday", "my keybase", "www.celestia.org", "ping @celestiaorg on twitter", "fake validator"),
					stakingtypes.NewCommissionRates(math.LegacyNewDecWithPrec(6, 0o2), math.LegacyNewDecWithPrec(12, 0o2), math.LegacyNewDecWithPrec(1, 0o2)),
					math.NewInt(1),
				)
				require.NoError(t, err)
				return []sdk.Msg{msg}, account
			},
			expectedCode: abci.CodeTypeOK,
		},
		{
			name: "create continuous vesting account with a start time in the future",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				vestAccName := "vesting"
				_, _, err := s.cctx.Keyring.NewMnemonic(vestAccName, keyring.English, "", "", hd.Secp256k1)
				require.NoError(t, err)
				sendAcc := s.unusedAccount()
				sendingAccAddr := testfactory.GetAddress(s.cctx.Keyring, sendAcc)
				vestAccAddr := testfactory.GetAddress(s.cctx.Keyring, vestAccName)
				msg := vestingtypes.NewMsgCreateVestingAccount(
					sendingAccAddr,
					vestAccAddr,
					sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewInt(1000000))),
					time.Now().Add(time.Hour).Unix(),
					time.Now().Add(time.Hour*2).Unix(),
					false,
				)
				return []sdk.Msg{msg}, sendAcc
			},
			expectedCode: abci.CodeTypeOK,
		},
		{
			name: "create community spend governance proposal",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				account := s.unusedAccount()
				// Note: this test depends on at least one coin being present
				// in the community pool. Funds land in the community pool due
				// to inflation so if 1 coin is not present in the community
				// pool, consider expanding the block interval or waiting for
				// more blocks to be produced prior to executing this test case.
				coins := sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewInt(1)))

				addr := testfactory.GetAddress(s.cctx.Keyring, account)
				msg, err := govv1.NewMsgSubmitProposal(
					[]sdk.Msg{
						disttypes.NewMsgFundCommunityPool(
							coins,
							authtypes.NewModuleAddress("gov").String(),
						),
					},
					sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewInt(1000000000))),
					addr.String(),
					"metadata",
					"title",
					"summary",
					false,
				)
				require.NoError(t, err)

				return []sdk.Msg{msg}, account
			},
			expectedCode: abci.CodeTypeOK,
		},
		{
			name: "create legacy text governance proposal",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				account := s.unusedAccount()
				content, ok := govv1beta1.ContentFromProposalType("title", "description", "text")
				require.True(t, ok)
				addr := testfactory.GetAddress(s.cctx.Keyring, account)
				msg, err := govv1beta1.NewMsgSubmitProposal(
					content,
					sdk.NewCoins(
						sdk.NewCoin(app.BondDenom, math.NewInt(1000000000))),
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
					testfactory.GetAddress(s.cctx.Keyring, account1),
					testfactory.GetAddress(s.cctx.Keyring, account2),
					sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewInt(1))),
				)
				account3 := s.unusedAccount()
				msgSend2 := banktypes.NewMsgSend(
					testfactory.GetAddress(s.cctx.Keyring, account1),
					testfactory.GetAddress(s.cctx.Keyring, account3),
					sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewInt(1))),
				)
				return []sdk.Msg{msgSend1, msgSend2}, account1
			},
			expectedCode: abci.CodeTypeOK,
		},
		// TODO: paramfilter module is removed, should be replaced by ante handler
		// {
		// 	name: "create param change proposal for a blocked parameter",
		// 	msgFunc: func() (msgs []sdk.Msg, signer string) {
		// 		account := s.unusedAccount()
		// 		change := proposal.NewParamChange(stakingtypes.ModuleName, string(stakingtypes.KeyBondDenom), "stake")
		// 		content := proposal.NewParameterChangeProposal("title", "description", []proposal.ParamChange{change})
		// 		addr := testfactory.GetAddress(s.cctx.Keyring, account)
		// 		msg, err := oldgov.NewMsgSubmitProposal(
		// 			content,
		// 			sdk.NewCoins(
		// 				sdk.NewCoin(app.BondDenom, math.NewInt(1000000000))),
		// 			addr,
		// 		)
		// 		require.NoError(t, err)
		// 		return []sdk.Msg{msg}, account
		// 	},
		// 	// this parameter is protected by the paramfilter module, and we
		// 	// should expect an error. Due to how errors are bubbled up, we get
		// 	// this code despite wrapping the expected error,
		// 	// paramfilter.ErrBlockedParameter
		// 	expectedCode: govtypes.ErrNoProposalHandlerExists.ABCICode(),
		// },
		{
			name: "create param proposal change for a modifiable parameter",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				account := s.unusedAccount()
				change := proposal.NewParamChange(stakingtypes.ModuleName, string(stakingtypes.KeyMaxValidators), "1")
				content := proposal.NewParameterChangeProposal("title", "description", []proposal.ParamChange{change})
				addr := testfactory.GetAddress(s.cctx.Keyring, account)
				msg, err := govv1beta1.NewMsgSubmitProposal(
					content,
					sdk.NewCoins(
						sdk.NewCoin(app.BondDenom, math.NewInt(1000000000))),
					addr,
				)
				require.NoError(t, err)
				return []sdk.Msg{msg}, account
			},
			expectedCode: abci.CodeTypeOK,
		},
		{
			name: "try to upgrade the network version",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				account := s.unusedAccount()
				addr := testfactory.GetAddress(s.cctx.Keyring, account)
				msg := signal.NewMsgTryUpgrade(addr)
				return []sdk.Msg{msg}, account
			},
			expectedCode: abci.CodeTypeOK,
		},
		{
			name: "signal a version change",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				valAccount := s.getValidatorAccount()
				msg := signal.NewMsgSignalVersion(valAccount.String(), appconsts.LatestVersion+1)
				return []sdk.Msg{msg}, s.getValidatorName()
			},
			expectedCode: abci.CodeTypeOK,
		},
	}

	// sign and submit the transactions
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serviceClient := sdktx.NewServiceClient(s.cctx.GRPCClient)
			msgs, signer := tt.msgFunc()
			txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(signer))
			require.NoError(t, err)
			res, err := txClient.SubmitTx(s.cctx.GoContext(), msgs, blobfactory.DefaultTxOpts()...)
			if tt.expectedCode != abci.CodeTypeOK {
				require.Error(t, err)
				require.Nil(t, res)
				txHash := err.(*user.ExecutionError).TxHash
				code := err.(*user.ExecutionError).Code
				getTxResp, err := serviceClient.GetTx(s.cctx.GoContext(), &sdktx.GetTxRequest{Hash: txHash})
				require.NoError(t, err)
				assert.Equal(t, tt.expectedCode, code, getTxResp.TxResponse.RawLog)
			} else {
				require.NoError(t, err)
				require.NotNil(t, res)
				getTxResp, err := serviceClient.GetTx(s.cctx.GoContext(), &sdktx.GetTxRequest{Hash: res.TxHash})
				require.NoError(t, err)
				assert.Equal(t, tt.expectedCode, res.Code, getTxResp.TxResponse.RawLog)
			}
		})
	}
}

func (s *StandardSDKIntegrationTestSuite) TestGRPCQueries() {
	t := s.T()
	t.Run("testnode can query network min gas price", func(t *testing.T) {
		queryClient := minfee.NewQueryClient(s.cctx.GRPCClient)
		resp, err := queryClient.NetworkMinGasPrice(s.cctx.GoContext(), &minfee.QueryNetworkMinGasPrice{})
		require.NoError(t, err)
		got, err := resp.NetworkMinGasPrice.Float64()
		require.NoError(t, err)
		assert.Equal(t, appconsts.DefaultNetworkMinGasPrice, got)
	})
	t.Run("testnode can query local min gas price", func(t *testing.T) {
		serviceClient := nodeservice.NewServiceClient(s.cctx.GRPCClient)
		resp, err := serviceClient.Config(s.cctx.GoContext(), &nodeservice.ConfigRequest{})
		require.NoError(t, err)
		want := "0.002000000000000000utia"
		assert.Equal(t, want, resp.MinimumGasPrice)
	})

	t.Run("testnode can query tx status", func(t *testing.T) {
		// Create a dummy tx hash
		dummyTxHash := "0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"

		// Create a new tx client
		txClient := tx.NewTxClient(s.cctx.GRPCClient)

		// Query for the tx status
		resp, err := txClient.TxStatus(s.cctx.GoContext(), &tx.TxStatusRequest{
			TxId: dummyTxHash,
		})
		require.NoError(t, err)
		assert.Equal(t, resp.Status, "UNKNOWN")

		txSubmitter, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg)
		require.NoError(t, err)
		blobs := blobfactory.RandV0BlobsWithNamespace([]share.Namespace{share.RandomNamespace()}, []int{1000})
		res, err := txSubmitter.SubmitPayForBlob(s.cctx.GoContext(), blobs, blobfactory.DefaultTxOpts()...)
		require.NoError(t, err)

		resp, err = txClient.TxStatus(s.cctx.GoContext(), &tx.TxStatusRequest{
			TxId: res.TxHash,
		})
		require.NoError(t, err)
		assert.Equal(t, resp.Status, "COMMITTED")
	})
}
