package user_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/math/unsafe"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/authz"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/app/grpc/gasestimation"
	"github.com/celestiaorg/celestia-app/v4/app/params"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/grpctest"
	"github.com/celestiaorg/celestia-app/v4/test/util/random"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
)

func TestTxClientTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}
	suite.Run(t, new(TxClientTestSuite))
}

type TxClientTestSuite struct {
	suite.Suite

	ctx           testnode.Context
	encCfg        encoding.Config
	txClient      *user.TxClient
	serviceClient sdktx.ServiceClient
}

func (suite *TxClientTestSuite) SetupSuite() {
	suite.encCfg, suite.txClient, suite.ctx = setupTxClient(suite.T(), testnode.DefaultTendermintConfig().Mempool.TTLDuration)
	suite.serviceClient = sdktx.NewServiceClient(suite.ctx.GRPCClient)
}

func (suite *TxClientTestSuite) TestSubmitPayForBlob() {
	t := suite.T()
	blobs := blobfactory.ManyRandBlobs(random.New(), 1e3, 1e4)

	subCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("submit blob without provided fee and gas limit", func(t *testing.T) {
		resp, err := suite.txClient.SubmitPayForBlob(subCtx, blobs)
		require.NoError(t, err)
		getTxResp, err := suite.serviceClient.GetTx(subCtx, &sdktx.GetTxRequest{Hash: resp.TxHash})
		require.NoError(t, err)
		require.EqualValues(t, 0, resp.Code)
		require.Greater(t, getTxResp.TxResponse.GasWanted, int64(0))
	})

	t.Run("submit blob with provided fee and gas limit", func(t *testing.T) {
		fee := user.SetFee(1e6)
		gas := user.SetGasLimit(1e6)
		resp, err := suite.txClient.SubmitPayForBlob(subCtx, blobs, fee, gas)
		require.NoError(t, err)
		getTxResp, err := suite.serviceClient.GetTx(subCtx, &sdktx.GetTxRequest{Hash: resp.TxHash})
		require.NoError(t, err)
		require.EqualValues(t, 0, resp.Code)
		require.EqualValues(t, getTxResp.TxResponse.GasWanted, 1e6)
	})

	t.Run("submit blob with different account", func(t *testing.T) {
		resp, err := suite.txClient.SubmitPayForBlobWithAccount(subCtx, "c", blobs, user.SetFee(1e6), user.SetGasLimit(1e6))
		require.NoError(t, err)
		getTxResp, err := suite.serviceClient.GetTx(subCtx, &sdktx.GetTxRequest{Hash: resp.TxHash})
		require.NoError(t, err)
		require.EqualValues(t, 0, resp.Code)
		require.EqualValues(t, getTxResp.TxResponse.GasWanted, 1e6)
	})

	t.Run("try submit a blob with an account that doesn't exist", func(t *testing.T) {
		_, err := suite.txClient.SubmitPayForBlobWithAccount(subCtx, "non-existent account", blobs)
		require.Error(t, err)
		require.Contains(t, err.Error(), "key not found")
	})
}

func (suite *TxClientTestSuite) TestSubmitTx() {
	t := suite.T()
	gasLimit := uint64(1e6)
	gasLimitOption := user.SetGasLimit(gasLimit)
	feeOption := user.SetFee(1e6)
	addr := suite.txClient.DefaultAddress()
	msg := bank.NewMsgSend(addr, testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(params.BondDenom, 10)))

	t.Run("submit tx without provided fee and gas limit", func(t *testing.T) {
		resp, err := suite.txClient.SubmitTx(suite.ctx.GoContext(), []sdk.Msg{msg})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code)
		getTxResp, err := suite.serviceClient.GetTx(suite.ctx.GoContext(), &sdktx.GetTxRequest{Hash: resp.TxHash})
		require.NoError(t, err)
		require.Greater(t, getTxResp.TxResponse.GasWanted, int64(0))
	})

	t.Run("submit tx with provided gas limit", func(t *testing.T) {
		resp, err := suite.txClient.SubmitTx(suite.ctx.GoContext(), []sdk.Msg{msg}, gasLimitOption)
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code)
		getTxResp, err := suite.serviceClient.GetTx(suite.ctx.GoContext(), &sdktx.GetTxRequest{Hash: resp.TxHash})
		require.NoError(t, err)
		require.EqualValues(t, int64(gasLimit), getTxResp.TxResponse.GasWanted)
	})

	t.Run("submit tx with provided fee", func(t *testing.T) {
		resp, err := suite.txClient.SubmitTx(suite.ctx.GoContext(), []sdk.Msg{msg}, feeOption)
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code)
	})

	t.Run("submit tx with provided fee and gas limit", func(t *testing.T) {
		resp, err := suite.txClient.SubmitTx(suite.ctx.GoContext(), []sdk.Msg{msg}, feeOption, gasLimitOption)
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code)
		getTxResp, err := suite.serviceClient.GetTx(suite.ctx.GoContext(), &sdktx.GetTxRequest{Hash: resp.TxHash})
		require.NoError(t, err)
		require.EqualValues(t, int64(gasLimit), getTxResp.TxResponse.GasWanted)
	})

	t.Run("submit tx with a different account", func(t *testing.T) {
		addr := suite.txClient.Account("b").Address()
		msg := bank.NewMsgSend(addr, testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(params.BondDenom, 10)))
		resp, err := suite.txClient.SubmitTx(suite.ctx.GoContext(), []sdk.Msg{msg})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code)
	})

	t.Run("submit tx with an updated default gas price", func(t *testing.T) {
		suite.txClient.SetDefaultGasPrice(appconsts.DefaultMinGasPrice / 2)
		resp, err := suite.txClient.SubmitTx(suite.ctx.GoContext(), []sdk.Msg{msg})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code)
		suite.txClient.SetDefaultGasPrice(appconsts.DefaultMinGasPrice)
	})
}

func (suite *TxClientTestSuite) TestConfirmTx() {
	t := suite.T()

	fee := user.SetFee(1e6)
	gas := user.SetGasLimit(1e6)

	t.Run("deadline exceeded when the context times out", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(suite.ctx.GoContext(), time.Second)
		defer cancel()

		seqBeforeBroadcast := suite.txClient.Signer().Account(suite.txClient.DefaultAccountName()).Sequence()
		msg := bank.NewMsgSend(suite.txClient.DefaultAddress(), testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(params.BondDenom, 10)))
		resp, err := suite.txClient.BroadcastTx(ctx, []sdk.Msg{msg})
		require.NoError(t, err)
		assertTxInTxTracker(t, suite.txClient, resp.TxHash, suite.txClient.DefaultAccountName(), seqBeforeBroadcast)

		_, err = suite.txClient.ConfirmTx(ctx, resp.TxHash)
		require.Error(t, err)
		require.Contains(t, err.Error(), context.DeadlineExceeded.Error())
	})

	t.Run("should error when tx is not found", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(suite.ctx.GoContext(), 5*time.Second)
		defer cancel()
		resp, err := suite.txClient.ConfirmTx(ctx, "E32BD15CAF57AF15D17B0D63CF4E63A9835DD1CEBB059C335C79586BC3013728")
		require.Contains(t, err.Error(), "transaction with hash E32BD15CAF57AF15D17B0D63CF4E63A9835DD1CEBB059C335C79586BC3013728 not found; it was likely rejected")
		require.Nil(t, resp)
	})

	t.Run("should return error log when execution fails", func(t *testing.T) {
		seqBeforeBroadcast := suite.txClient.Signer().Account(suite.txClient.DefaultAccountName()).Sequence()
		innerMsg := bank.NewMsgSend(testnode.RandomAddress().(sdk.AccAddress), testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(params.BondDenom, 10)))
		msg := authz.NewMsgExec(suite.txClient.DefaultAddress(), []sdk.Msg{innerMsg})
		resp, err := suite.txClient.BroadcastTx(suite.ctx.GoContext(), []sdk.Msg{&msg}, fee, gas)
		require.NoError(t, err)
		assertTxInTxTracker(t, suite.txClient, resp.TxHash, suite.txClient.DefaultAccountName(), seqBeforeBroadcast)

		confirmTxResp, err := suite.txClient.ConfirmTx(suite.ctx.GoContext(), resp.TxHash)
		require.Error(t, err)
		require.Contains(t, err.Error(), "authorization not found")
		require.Nil(t, confirmTxResp)
		require.True(t, wasRemovedFromTxTracker(resp.TxHash, suite.txClient))
	})

	t.Run("should success when tx is found immediately", func(t *testing.T) {
		addr := suite.txClient.DefaultAddress()
		seqBeforeBroadcast := suite.txClient.Signer().Account(suite.txClient.DefaultAccountName()).Sequence()
		msg := bank.NewMsgSend(addr, testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(params.BondDenom, 10)))
		resp, err := suite.txClient.BroadcastTx(suite.ctx.GoContext(), []sdk.Msg{msg}, fee, gas)
		require.NoError(t, err)
		require.Equal(t, resp.Code, abci.CodeTypeOK)
		assertTxInTxTracker(t, suite.txClient, resp.TxHash, suite.txClient.DefaultAccountName(), seqBeforeBroadcast)

		ctx, cancel := context.WithTimeout(suite.ctx.GoContext(), 30*time.Second)
		defer cancel()
		confirmTxResp, err := suite.txClient.ConfirmTx(ctx, resp.TxHash)
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, confirmTxResp.Code)
		require.True(t, wasRemovedFromTxTracker(resp.TxHash, suite.txClient))
	})

	t.Run("should error when tx is found with a non-zero error code", func(t *testing.T) {
		balance := suite.queryCurrentBalance(t)
		addr := suite.txClient.DefaultAddress()
		seqBeforeBroadcast := suite.txClient.Signer().Account(suite.txClient.DefaultAccountName()).Sequence()
		// Create a msg send with out of balance, ensure this tx fails
		msg := bank.NewMsgSend(addr, testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(params.BondDenom, 1+balance)))
		resp, err := suite.txClient.BroadcastTx(suite.ctx.GoContext(), []sdk.Msg{msg}, fee, gas)
		require.NoError(t, err)
		require.Equal(t, resp.Code, abci.CodeTypeOK)
		assertTxInTxTracker(t, suite.txClient, resp.TxHash, suite.txClient.DefaultAccountName(), seqBeforeBroadcast)

		confirmTxResp, err := suite.txClient.ConfirmTx(suite.ctx.GoContext(), resp.TxHash)
		require.Error(t, err)
		require.Nil(t, confirmTxResp)
		code := err.(*user.ExecutionError).Code
		require.NotEqual(t, abci.CodeTypeOK, code)
		require.True(t, wasRemovedFromTxTracker(resp.TxHash, suite.txClient))
	})
}

func TestEvictions(t *testing.T) {
	_, txClient, ctx := setupTxClient(t, 1*time.Nanosecond)

	fee := user.SetFee(1e6)
	gas := user.SetGasLimit(1e6)

	// Keep submitting the transaction until we get the eviction error
	sender := txClient.Signer().Account(txClient.DefaultAccountName())
	msg := bank.NewMsgSend(sender.Address(), testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(params.BondDenom, 10)))
	var seqBeforeEviction uint64
	// Loop five times until the tx is evicted
	for i := 0; i < 5; i++ {
		seqBeforeEviction = sender.Sequence()
		resp, err := txClient.BroadcastTx(ctx.GoContext(), []sdk.Msg{msg}, fee, gas)
		require.NoError(t, err)
		_, err = txClient.ConfirmTx(ctx.GoContext(), resp.TxHash)
		if err != nil {
			if err.Error() == "tx was evicted from the mempool" {
				break
			}
		}
	}

	seqAfterEviction := sender.Sequence()
	require.Equal(t, seqBeforeEviction, seqAfterEviction)
}

// TestWithEstimatorService ensures that if the WithEstimatorService
// option is provided to the tx client, the separate gas estimator service is
// used to estimate gas price and usage instead of the default connection.
func TestWithEstimatorService(t *testing.T) {
	mockEstimator := setupEstimatorService(t)
	_, txClient, ctx := setupTxClient(t, testnode.DefaultTendermintConfig().Mempool.TTLDuration, user.WithEstimatorService(mockEstimator.conn))

	msg := bank.NewMsgSend(txClient.DefaultAddress(), testnode.RandomAddress().(sdk.AccAddress),
		sdk.NewCoins(sdk.NewInt64Coin(params.BondDenom, 10)))
	price, used, err := txClient.EstimateGasPriceAndUsage(ctx.GoContext(), []sdk.Msg{msg}, 1)
	require.NoError(t, err)

	assert.Equal(t, 0.02, price)
	assert.Equal(t, uint64(70000), used)
}

func (suite *TxClientTestSuite) TestGasPriceAndUsageEstimation() {
	addr := suite.txClient.DefaultAddress()
	msg := bank.NewMsgSend(addr, testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(params.BondDenom, 10)))
	gasPrice, gasUsage, err := suite.txClient.EstimateGasPriceAndUsage(suite.ctx.GoContext(), []sdk.Msg{msg}, 1)
	require.NoError(suite.T(), err)
	require.Greater(suite.T(), gasPrice, float64(0))
	require.Greater(suite.T(), gasUsage, uint64(0))
}

func (suite *TxClientTestSuite) TestGasPriceEstimation() {
	gasPrice, err := suite.txClient.EstimateGasPrice(suite.ctx.GoContext(), 0)
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), gasPrice, appconsts.DefaultMinGasPrice)
}

// TestGasConsumption verifies that the amount deducted from a user's balance is
// based on the fee provided in the tx instead of the gas used by the tx. This
// behavior leads to poor UX because tx submitters must over-estimate the amount
// of gas that their tx will consume and they are not refunded for the excessuite.
func (suite *TxClientTestSuite) TestGasConsumption() {
	t := suite.T()

	utiaToSend := int64(1)
	addr := suite.txClient.DefaultAddress()
	msg := bank.NewMsgSend(addr, testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(params.BondDenom, utiaToSend)))

	gasPrice := int64(1)
	gasLimit := uint64(1e6)
	fee := uint64(1e6) // 1 TIA
	// Note: gas price * gas limit = fee amount. So by setting gasLimit and fee
	// to the same value, these options set a gas price of 1utia.
	options := []user.TxOption{user.SetGasLimit(gasLimit), user.SetFee(fee)}

	balanceBefore := suite.queryCurrentBalance(t)
	resp, err := suite.txClient.SubmitTx(suite.ctx.GoContext(), []sdk.Msg{msg}, options...)
	require.NoError(t, err)

	require.EqualValues(t, abci.CodeTypeOK, resp.Code)
	balanceAfter := suite.queryCurrentBalance(t)

	// verify that the amount deducted depends on the fee set in the tx.
	amountDeducted := balanceBefore - balanceAfter - utiaToSend
	require.Equal(t, int64(fee), amountDeducted)

	res, err := suite.serviceClient.GetTx(suite.ctx.GoContext(), &sdktx.GetTxRequest{Hash: resp.TxHash})
	require.NoError(t, err)

	// verify that the amount deducted does not depend on the actual gas used.
	gasUsedBasedDeduction := res.TxResponse.GasUsed * gasPrice
	require.NotEqual(t, gasUsedBasedDeduction, amountDeducted)
	// The gas used based deduction should be less than the fee because the fee is 1 TIA.
	require.Less(t, gasUsedBasedDeduction, int64(fee))
}

func (suite *TxClientTestSuite) TestTxClientWithDifferentDefaultAccount() {
	txClient, err := user.SetupTxClient(suite.ctx.GoContext(), suite.ctx.Keyring, suite.ctx.GRPCClient, suite.encCfg, user.WithDefaultAccount("b"))
	suite.NoError(err)
	suite.Equal(txClient.DefaultAccountName(), "b")

	addrC := txClient.Account("c").Address()
	txClient, err = user.SetupTxClient(suite.ctx.GoContext(), suite.ctx.Keyring, suite.ctx.GRPCClient, suite.encCfg, user.WithDefaultAddress(addrC))
	suite.NoError(err)
	suite.Equal(txClient.DefaultAddress(), addrC)
}

func (suite *TxClientTestSuite) queryCurrentBalance(t *testing.T) int64 {
	balanceQuery := bank.NewQueryClient(suite.ctx.GRPCClient)
	addr := suite.txClient.DefaultAddress()
	balanceResp, err := balanceQuery.AllBalances(suite.ctx.GoContext(), &bank.QueryAllBalancesRequest{Address: addr.String()})
	require.NoError(t, err)
	return balanceResp.Balances.AmountOf(params.BondDenom).Int64()
}

func wasRemovedFromTxTracker(txHash string, txClient *user.TxClient) bool {
	seq, signer, exists := txClient.GetTxFromTxTracker(txHash)
	return !exists && seq == 0 && signer == ""
}

// asserts that a tx was indexed in the tx tracker and that the sequence does not increase
func assertTxInTxTracker(t *testing.T, txClient *user.TxClient, txHash, expectedSigner string, seqBeforeBroadcast uint64) {
	seqFromTxTracker, signer, exists := txClient.GetTxFromTxTracker(txHash)
	require.True(t, exists)
	require.Equal(t, expectedSigner, signer)
	seqAfterBroadcast := txClient.Signer().Account(expectedSigner).Sequence()
	// TxInfo is indexed before the nonce is increased
	require.Equal(t, seqBeforeBroadcast, seqFromTxTracker)
	// Successfully broadcast transaction increases the sequence
	require.Equal(t, seqAfterBroadcast, seqBeforeBroadcast+1)
}

func setupTxClient(
	t *testing.T,
	ttlDuration time.Duration,
	opts ...user.Option,
) (encoding.Config, *user.TxClient, testnode.Context) {
	defaultTmConfig := testnode.DefaultTendermintConfig()
	defaultTmConfig.Mempool.TTLDuration = ttlDuration

	chainID := unsafe.Str(6)
	testnodeConfig := testnode.DefaultConfig().
		WithTendermintConfig(defaultTmConfig).
		WithFundedAccounts("a", "b", "c").
		WithChainID(chainID).
		WithTimeoutCommit(100 * time.Millisecond).
		WithAppCreator(testnode.CustomAppCreator(baseapp.SetMinGasPrices("0utia"), baseapp.SetChainID(chainID)))

	ctx, _, _ := testnode.NewNetwork(t, testnodeConfig)
	_, err := ctx.WaitForHeight(1)
	require.NoError(t, err)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	txClient, err := user.SetupTxClient(ctx.GoContext(), ctx.Keyring, ctx.GRPCClient, enc, opts...)
	require.NoError(t, err)

	return enc, txClient, ctx
}

type mockEstimatorServer struct {
	*gasestimation.UnimplementedGasEstimatorServer
	srv  *grpc.Server
	conn *grpc.ClientConn
	addr string
}

func (m *mockEstimatorServer) EstimateGasPriceAndUsage(
	context.Context,
	*gasestimation.EstimateGasPriceAndUsageRequest,
) (*gasestimation.EstimateGasPriceAndUsageResponse, error) {
	return &gasestimation.EstimateGasPriceAndUsageResponse{
		EstimatedGasPrice: 0.02,
		EstimatedGasUsed:  70000,
	}, nil
}

func (m *mockEstimatorServer) stop() {
	m.srv.GracefulStop()
}

func setupEstimatorService(t *testing.T) *mockEstimatorServer {
	t.Helper()

	freePort, err := testnode.GetFreePort()
	require.NoError(t, err)
	addr := fmt.Sprintf(":%d", freePort)
	net, err := net.Listen("tcp", addr)
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	mes := &mockEstimatorServer{srv: grpcServer, addr: addr}
	gasestimation.RegisterGasEstimatorServer(grpcServer, mes)

	go func() {
		err := grpcServer.Serve(net)
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			panic(err)
		}
	}()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() {
		err = conn.Close()
		require.NoError(t, err)
	})
	mes.conn = conn

	t.Cleanup(mes.stop)
	return mes
}

const (
	BroadcastTestChainID = "test-broadcast-chain"
	bufferSize           = 1024 * 1024
)

type BroadcastTestSuite struct {
	suite.Suite
	kr      keyring.Keyring
	encCfg  encoding.Config
	signer  *user.Signer
	account string
	accAddr sdk.AccAddress
}

func (s *BroadcastTestSuite) SetupSuite() {
	s.encCfg = encoding.MakeConfig()
	s.kr = keyring.NewInMemory(s.encCfg.Codec)
	s.account = "test_broadcast_account"
	info, _, err := s.kr.NewMnemonic(s.account, keyring.English, "", "", hd.Secp256k1)
	s.Require().NoError(err)
	pubKey, err := info.GetPubKey()
	s.Require().NoError(err)
	s.accAddr = sdk.AccAddress(pubKey.Address())

	s.signer, err = user.NewSigner(s.kr, s.encCfg.TxConfig, BroadcastTestChainID, user.NewAccount(s.account, 0, 0))
	s.Require().NoError(err)
}

func TestBroadcastTestSuite(t *testing.T) {
	suite.Run(t, new(BroadcastTestSuite))
}

// Helper to create a TxClient specifically for broadcast tests, using mock connections.
func (s *BroadcastTestSuite) setupTestClient(t *testing.T, conns []*grpc.ClientConn) *user.TxClient {
	t.Helper()
	require.NotEmpty(t, conns, "Need at least one connection for TxClient")

	primaryConn := conns[0]
	otherConns := conns[1:]

	txClient, err := user.NewTxClient(
		s.encCfg.Codec,
		s.signer,
		primaryConn,
		s.encCfg.InterfaceRegistry,
		user.WithAdditionalCoreEndpoints(otherConns),
		user.WithDefaultAccount(s.account),
	)
	require.NoError(t, err)
	return txClient
}

type broadcastTestCase struct {
	name string
	// Function to set up mock servers and return their handlers + connections
	setupMocks        func(t *testing.T) ([]*grpctest.MockTxService, []*grpc.ClientConn, []func())
	setupContext      func() (context.Context, context.CancelFunc)
	txOpts            []user.TxOption
	expectedSuccess   bool
	expectedHash      string // Only checked if expectedSuccess is true
	expectedErrSubstr string // Only checked if expectedSuccess is false
	expectedErrType   error  // Optional: Specific error type check (e.g., context.DeadlineExceeded)
	// Check exact invocation count is difficult due to races with cancellation.
	// We focus on the winner and whether unused mocks were invoked.
	checkMockInvocations func(t *testing.T, mocks []*grpctest.MockTxService) // Custom checks if needed
}

func (s *BroadcastTestSuite) TestBroadcastScenarios() {
	t := s.T()

	// Default options for most tests
	defaultOpts := []user.TxOption{user.SetGasLimit(100000), user.SetFee(1000)}
	// Basic MsgSend for testing
	defaultMsg := bank.NewMsgSend(s.accAddr, s.accAddr, sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(10))))

	testCases := []broadcastTestCase{
		{
			name: "Primary Success (Simplified - Single Conn)",
			setupMocks: func(t *testing.T) ([]*grpctest.MockTxService, []*grpc.ClientConn, []func()) {
				// Only setup ONE mock server
				mockSvc1 := &grpctest.MockTxService{
					BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
						return &sdktx.BroadcastTxResponse{TxResponse: &sdk.TxResponse{Code: abci.CodeTypeOK, TxHash: "HASH1"}}, nil
					},
				}
				conn1, stop1 := grpctest.StartMockServer(t, mockSvc1)
				// Return only the single mock, connection, and stopper
				return []*grpctest.MockTxService{mockSvc1},
					[]*grpc.ClientConn{conn1},
					[]func(){stop1}
			},
			setupContext: func() (context.Context, context.CancelFunc) {
				return context.Background(), func() {} // No deadline needed
			},
			txOpts:          defaultOpts,
			expectedSuccess: true,
			expectedHash:    "HASH1",
			checkMockInvocations: func(t *testing.T, mocks []*grpctest.MockTxService) {
				require.Len(t, mocks, 1)
				require.Equal(t, int32(1), mocks[0].Invocations.Load(), "Mock1 (single) should be invoked once")
			},
		},
		{
			name: "Secondary Success",
			setupMocks: func(t *testing.T) ([]*grpctest.MockTxService, []*grpc.ClientConn, []func()) {
				mockSvc1 := &grpctest.MockTxService{ // Primary fails after delay
					BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
						time.Sleep(50 * time.Millisecond)
						return nil, errors.New("mock1 failed")
					},
				}
				mockSvc2 := &grpctest.MockTxService{ // Secondary succeeds quickly
					BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
						time.Sleep(10 * time.Millisecond)
						return &sdktx.BroadcastTxResponse{TxResponse: &sdk.TxResponse{Code: abci.CodeTypeOK, TxHash: "HASH2"}}, nil
					},
				}
				mockSvc3 := &grpctest.MockTxService{ // Tertiary should be cancelled
					BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
						select {
						case <-time.After(500 * time.Millisecond):
							return nil, errors.New("mock3 should have been cancelled")
						case <-ctx.Done():
							return nil, ctx.Err()
						}
					},
				}
				conn1, stop1 := grpctest.StartMockServer(t, mockSvc1)
				conn2, stop2 := grpctest.StartMockServer(t, mockSvc2)
				conn3, stop3 := grpctest.StartMockServer(t, mockSvc3)
				return []*grpctest.MockTxService{mockSvc1, mockSvc2, mockSvc3},
					[]*grpc.ClientConn{conn1, conn2, conn3},
					[]func(){stop1, stop2, stop3}
			},
			setupContext: func() (context.Context, context.CancelFunc) {
				return context.Background(), func() {}
			},
			txOpts:          defaultOpts,
			expectedSuccess: true,
			expectedHash:    "HASH2",
			checkMockInvocations: func(t *testing.T, mocks []*grpctest.MockTxService) {
				require.Len(t, mocks, 3)
				require.GreaterOrEqual(t, mocks[0].Invocations.Load(), int32(1)) // Should be invoked
				require.Equal(t, int32(1), mocks[1].Invocations.Load())          // Winner should be invoked
				require.GreaterOrEqual(t, mocks[2].Invocations.Load(), int32(0)) // May or may not be invoked before cancellation
			},
		},
		{
			name: "All Fail",
			setupMocks: func(t *testing.T) ([]*grpctest.MockTxService, []*grpc.ClientConn, []func()) {
				err1 := errors.New("err1")
				err2 := errors.New("err2")
				err3 := errors.New("err3")
				mockSvc1 := &grpctest.MockTxService{BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
					return nil, err1
				}}
				mockSvc2 := &grpctest.MockTxService{BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
					return nil, err2
				}}
				mockSvc3 := &grpctest.MockTxService{BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
					return nil, err3
				}}

				conn1, stop1 := grpctest.StartMockServer(t, mockSvc1)
				conn2, stop2 := grpctest.StartMockServer(t, mockSvc2)
				conn3, stop3 := grpctest.StartMockServer(t, mockSvc3)
				return []*grpctest.MockTxService{mockSvc1, mockSvc2, mockSvc3},
					[]*grpc.ClientConn{conn1, conn2, conn3},
					[]func(){stop1, stop2, stop3}
			},
			setupContext: func() (context.Context, context.CancelFunc) {
				return context.Background(), func() {}
			},
			txOpts:            defaultOpts,
			expectedSuccess:   false,
			expectedErrSubstr: "err",
			checkMockInvocations: func(t *testing.T, mocks []*grpctest.MockTxService) {
				require.Len(t, mocks, 3)
				require.Equal(t, int32(1), mocks[0].Invocations.Load())
				require.Equal(t, int32(1), mocks[1].Invocations.Load())
				require.Equal(t, int32(1), mocks[2].Invocations.Load())
			},
		},
		{
			name: "Context Deadline",
			setupMocks: func(t *testing.T) ([]*grpctest.MockTxService, []*grpc.ClientConn, []func()) {
				mockHandler := func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
					select {
					case <-time.After(200 * time.Millisecond): // Longer than deadline
						return nil, errors.New("mock should have been cancelled by deadline")
					case <-ctx.Done():
						return nil, ctx.Err() // Correctly return context error
					}
				}
				mockSvc1 := &grpctest.MockTxService{BroadcastHandler: mockHandler}
				mockSvc2 := &grpctest.MockTxService{BroadcastHandler: mockHandler}
				mockSvc3 := &grpctest.MockTxService{BroadcastHandler: mockHandler}
				conn1, stop1 := grpctest.StartMockServer(t, mockSvc1)
				conn2, stop2 := grpctest.StartMockServer(t, mockSvc2)
				conn3, stop3 := grpctest.StartMockServer(t, mockSvc3)
				return []*grpctest.MockTxService{mockSvc1, mockSvc2, mockSvc3},
					[]*grpc.ClientConn{conn1, conn2, conn3},
					[]func(){stop1, stop2, stop3}
			},
			setupContext: func() (context.Context, context.CancelFunc) {
				// Short deadline
				return context.WithTimeout(context.Background(), 50*time.Millisecond)
			},
			txOpts:          defaultOpts,
			expectedSuccess: false,
			expectedErrType: context.DeadlineExceeded, // Check for specific error type
			checkMockInvocations: func(t *testing.T, mocks []*grpctest.MockTxService) {
				// Invocations might be 0 or 1 depending on race with deadline
				require.Len(t, mocks, 3)
			},
		},
		{
			name: "Less Than Three Conns (Success)",
			setupMocks: func(t *testing.T) ([]*grpctest.MockTxService, []*grpc.ClientConn, []func()) {
				mockSvc1 := &grpctest.MockTxService{BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
					return nil, errors.New("err1")
				}}
				mockSvc2 := &grpctest.MockTxService{ // Succeeds
					BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
						return &sdktx.BroadcastTxResponse{TxResponse: &sdk.TxResponse{Code: abci.CodeTypeOK, TxHash: "HASH_LT3"}}, nil
					},
				}
				conn1, stop1 := grpctest.StartMockServer(t, mockSvc1)
				conn2, stop2 := grpctest.StartMockServer(t, mockSvc2)
				return []*grpctest.MockTxService{mockSvc1, mockSvc2},
					[]*grpc.ClientConn{conn1, conn2},
					[]func(){stop1, stop2}
			},
			setupContext: func() (context.Context, context.CancelFunc) {
				return context.Background(), func() {}
			},
			txOpts:          defaultOpts,
			expectedSuccess: true,
			expectedHash:    "HASH_LT3",
			checkMockInvocations: func(t *testing.T, mocks []*grpctest.MockTxService) {
				require.Len(t, mocks, 2)
				require.Equal(t, int32(1), mocks[0].Invocations.Load())
				require.Equal(t, int32(1), mocks[1].Invocations.Load())
			},
		},
		{
			name: "More Than Three Conns (Success)",
			setupMocks: func(t *testing.T) ([]*grpctest.MockTxService, []*grpc.ClientConn, []func()) {
				mockSvc1 := &grpctest.MockTxService{BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
					return nil, errors.New("err1")
				}}
				mockSvc2 := &grpctest.MockTxService{ // Succeeds
					BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
						return &sdktx.BroadcastTxResponse{TxResponse: &sdk.TxResponse{Code: abci.CodeTypeOK, TxHash: "HASH_MT3"}}, nil
					},
				}
				mockSvc3 := &grpctest.MockTxService{BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
					return nil, errors.New("err3")
				}}
				mockSvc4 := &grpctest.MockTxService{ // Should NOT be called
					BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
						t.Error("Mock service 4 should not have been called")
						return nil, errors.New("err4 - should not happen")
					},
				}
				conn1, stop1 := grpctest.StartMockServer(t, mockSvc1)
				conn2, stop2 := grpctest.StartMockServer(t, mockSvc2)
				conn3, stop3 := grpctest.StartMockServer(t, mockSvc3)
				conn4, stop4 := grpctest.StartMockServer(t, mockSvc4) // Setup 4th connection
				return []*grpctest.MockTxService{mockSvc1, mockSvc2, mockSvc3, mockSvc4},
					[]*grpc.ClientConn{conn1, conn2, conn3, conn4},
					[]func(){stop1, stop2, stop3, stop4}
			},
			setupContext: func() (context.Context, context.CancelFunc) {
				return context.Background(), func() {}
			},
			txOpts:          defaultOpts,
			expectedSuccess: true,
			expectedHash:    "HASH_MT3",
			checkMockInvocations: func(t *testing.T, mocks []*grpctest.MockTxService) {
				require.Len(t, mocks, 4)
				// broadcastTx internally selects only up to 3 connections.
				require.Equal(t, int32(1), mocks[0].Invocations.Load())
				require.Equal(t, int32(1), mocks[1].Invocations.Load())
				require.Equal(t, int32(1), mocks[2].Invocations.Load())
				require.Equal(t, int32(0), mocks[3].Invocations.Load(), "Mock4 should not be invoked")
			},
		},
		// Add more cases as needed (e.g., non-zero TxResponse code failure)
		{
			name: "Non-Zero Code Failure",
			setupMocks: func(t *testing.T) ([]*grpctest.MockTxService, []*grpc.ClientConn, []func()) {
				mockSvc1 := &grpctest.MockTxService{
					BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
						return &sdktx.BroadcastTxResponse{TxResponse: &sdk.TxResponse{Code: 5, TxHash: "HASH_FAIL", RawLog: "insufficient funds"}}, nil // Non-zero code
					},
				}
				conn1, stop1 := grpctest.StartMockServer(t, mockSvc1)
				return []*grpctest.MockTxService{mockSvc1}, []*grpc.ClientConn{conn1}, []func(){stop1}
			},
			setupContext: func() (context.Context, context.CancelFunc) {
				return context.Background(), func() {}
			},
			txOpts:            defaultOpts,
			expectedSuccess:   false,
			expectedErrSubstr: "insufficient funds", // Check for the RawLog in the error
			expectedErrType:   &user.BroadcastTxError{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mocks, conns, stops := tc.setupMocks(t)
			for _, stop := range stops {
				defer stop()
			}

			txClient := s.setupTestClient(t, conns)

			ctx, cancel := tc.setupContext()
			defer cancel()

			resp, err := txClient.BroadcastTx(ctx, []sdk.Msg{defaultMsg}, tc.txOpts...)

			if tc.expectedSuccess {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.Equal(t, abci.CodeTypeOK, resp.Code)
				require.Equal(t, tc.expectedHash, resp.TxHash)
			} else {
				require.Error(t, err)
				require.Nil(t, resp)
				if tc.expectedErrSubstr != "" {
					require.Contains(t, err.Error(), tc.expectedErrSubstr)
				}
				if tc.expectedErrType != nil {
					// Use require.ErrorIs for checking specific error values like context errors
					if errors.Is(tc.expectedErrType, context.DeadlineExceeded) || errors.Is(tc.expectedErrType, context.Canceled) {
						require.True(t, errors.Is(err, tc.expectedErrType), "Expected error chain to contain %v, but got %v", tc.expectedErrType, err)
					} else {
						// Use require.ErrorAs for checking error types (structs/interfaces)
						require.ErrorAs(t, err, &tc.expectedErrType)
					}
				}
			}

			if tc.checkMockInvocations != nil {
				mockServices := make([]*grpctest.MockTxService, len(mocks))
				copy(mockServices, mocks)
				tc.checkMockInvocations(t, mockServices)
			}
		})
	}
}
