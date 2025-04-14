package user_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"cosmossdk.io/math/unsafe"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
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
	enc := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)

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
