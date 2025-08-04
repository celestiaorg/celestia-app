package user_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/math/unsafe"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/app/grpc/gasestimation"
	"github.com/celestiaorg/celestia-app/v6/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v6/app/params"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/celestia-app/v6/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v6/test/util/grpctest"
	"github.com/celestiaorg/celestia-app/v6/test/util/random"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/rpc/core"
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
	suite.encCfg, suite.txClient, suite.ctx = setupTxClientWithDefaultParams(suite.T())
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
		require.Contains(t, err.Error(), "transaction with hash E32BD15CAF57AF15D17B0D63CF4E63A9835DD1CEBB059C335C79586BC3013728 not found")
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

func TestRejections(t *testing.T) {
	ttlNumBlocks := int64(5)
	_, txClient, ctx := setupTxClient(t, ttlNumBlocks, appconsts.DefaultMaxBytes)

	fee := user.SetFee(1e6)
	gas := user.SetGasLimit(1e6)

	// Submit a blob tx with user set ttl. After the ttl expires, the tx will be rejected.
	timeoutHeight := uint64(1)
	sender := txClient.Signer().Account(txClient.DefaultAccountName())
	seqBeforeSubmission := sender.Sequence()
	blobs := blobfactory.ManyRandBlobs(random.New(), 2, 2)
	resp, err := txClient.BroadcastPayForBlob(ctx.GoContext(), blobs, fee, gas, user.SetTimeoutHeight(timeoutHeight))
	require.NoError(t, err)

	require.NoError(t, ctx.WaitForBlocks(1)) // Skip one block to allow the tx to be rejected

	_, err = txClient.ConfirmTx(ctx.GoContext(), resp.TxHash)
	require.Error(t, err)
	require.Contains(t, err.Error(), "was rejected by the node")
	seqAfterRejection := sender.Sequence()
	require.Equal(t, seqBeforeSubmission, seqAfterRejection)

	// Now submit the same blob transaction again
	submitBlobResp, err := txClient.SubmitPayForBlob(ctx.GoContext(), blobs, fee, gas)
	require.NoError(t, err)
	require.Equal(t, submitBlobResp.Code, abci.CodeTypeOK)
	// Sequence should have increased
	seqAfterConfirmation := sender.Sequence()
	require.Equal(t, seqBeforeSubmission+1, seqAfterConfirmation)
	// Was removed from the tx tracker
	_, _, exists := txClient.GetTxFromTxTracker(resp.TxHash)
	require.False(t, exists)
}

func TestEvictions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping evictions test in short mode")
	}

	ttlNumBlocks := int64(1)
	blocksize := int64(1048576) // 1 MiB
	_, txClient, ctx := setupTxClient(t, ttlNumBlocks, blocksize)
	grpcTxClient := tx.NewTxClient(ctx.GRPCClient)

	fee := user.SetFee(1e6)
	gas := user.SetGasLimit(10e6)

	responses := make([]*sdk.TxResponse, 10)

	// Submit more transactions than a single block can fit with a 1-block TTL.
	// Txs will be evicted from the mempool and automatically resubmitted by the txClient during confirm().
	for i := 0; i < len(responses); i++ {
		blobs := blobfactory.ManyRandBlobs(random.New(), 500000, 500000, 5000) // ~1MiB per transaction
		resp, err := txClient.BroadcastPayForBlob(ctx.GoContext(), blobs, fee, gas)
		require.NoError(t, err)
		require.Equal(t, resp.Code, abci.CodeTypeOK)
		responses[i] = resp
	}

	evictedTxHashes := make([]string, 0)
	for _, resp := range responses {
		// Check txs for eviction and save them for confirmation verification later
		txInfo, err := grpcTxClient.TxStatus(ctx.GoContext(), &tx.TxStatusRequest{TxId: resp.TxHash})
		require.NoError(t, err)
		if txInfo.Status == core.TxStatusEvicted {
			evictedTxHashes = append(evictedTxHashes, resp.TxHash)
		}

		// Confirm should see they were evicted and automatically resubmit
		res, err := txClient.ConfirmTx(ctx.GoContext(), resp.TxHash)
		require.NoError(t, err)
		require.Equal(t, res.Code, abci.CodeTypeOK)
		// They should be removed from the tx tracker after confirmation
		_, _, exists := txClient.GetTxFromTxTracker(resp.TxHash)
		require.False(t, exists)
	}

	// At least 8 txs should have been evicted and resubmitted
	require.GreaterOrEqual(t, len(evictedTxHashes), 8)

	// Re-query evicted tx hashes and assert that they are now committed
	for _, txHash := range evictedTxHashes {
		txInfo, err := grpcTxClient.TxStatus(ctx.GoContext(), &tx.TxStatusRequest{TxId: txHash})
		require.NoError(t, err)
		require.Equal(t, txInfo.Status, core.TxStatusCommitted)
	}
}

// TestWithEstimatorService ensures that if the WithEstimatorService
// option is provided to the tx client, the separate gas estimator service is
// used to estimate gas price and usage instead of the default connection.
func TestWithEstimatorService(t *testing.T) {
	mockEstimator := setupEstimatorService(t)
	_, txClient, ctx := setupTxClientWithDefaultParams(t, user.WithEstimatorService(mockEstimator.conn))

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
// of gas that their tx will consume and they are not refunded for the excess.
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
	ttlNumBlocks int64,
	blocksize int64,
	opts ...user.Option,
) (encoding.Config, *user.TxClient, testnode.Context) {
	defaultTmConfig := testnode.DefaultTendermintConfig()
	defaultTmConfig.Mempool.TTLNumBlocks = ttlNumBlocks

	chainID := unsafe.Str(6)
	testnodeConfig := testnode.DefaultConfig().
		WithTendermintConfig(defaultTmConfig).
		WithFundedAccounts("a", "b", "c").
		WithChainID(chainID).
		WithTimeoutCommit(100 * time.Millisecond).
		WithAppCreator(testnode.CustomAppCreator(baseapp.SetMinGasPrices(fmt.Sprintf("%v%v", appconsts.DefaultMinGasPrice, appconsts.BondDenom)), baseapp.SetChainID(chainID)))
	testnodeConfig.Genesis.ConsensusParams.Block.MaxBytes = blocksize

	ctx, _, _ := testnode.NewNetwork(t, testnodeConfig)
	_, err := ctx.WaitForHeight(1)
	require.NoError(t, err)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	txClient, err := user.SetupTxClient(ctx.GoContext(), ctx.Keyring, ctx.GRPCClient, enc, opts...)
	require.NoError(t, err)

	return enc, txClient, ctx
}

func setupTxClientWithDefaultParams(t *testing.T, opts ...user.Option) (encoding.Config, *user.TxClient, testnode.Context) {
	return setupTxClient(t, 0, 8388608, opts...) // no ttl and 8MiB block size
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

	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(math.MaxInt32),
			grpc.MaxCallRecvMsgSize(math.MaxInt32),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		err = conn.Close()
		require.NoError(t, err)
	})
	mes.conn = conn

	t.Cleanup(mes.stop)
	return mes
}

var (
	errMock1             = errors.New("mock1 failed")
	errMock2             = errors.New("mock2 failed")
	errMock3             = errors.New("mock3 failed")
	errInsufficientFunds = errors.New("insufficient funds") // Replicates SDK error text
)

type broadcastTestCase struct {
	setupMocks  func(t *testing.T) ([]*grpctest.MockTxService, []*grpc.ClientConn)
	expectError bool // Changed from error to bool
}

func (suite *TxClientTestSuite) TestMultiConnBroadcast() {
	t := suite.T()

	// Default options for most tests - used only to create a valid tx.
	defaultOpts := []user.TxOption{user.SetGasLimit(100000), user.SetFee(1000)}
	// Basic MsgSend for testing - use the main suite's default address.
	defaultMsg := bank.NewMsgSend(suite.txClient.DefaultAddress(), suite.txClient.DefaultAddress(), sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(10))))

	testCases := []broadcastTestCase{
		{ // Primary Success (Single Conn)
			setupMocks: func(t *testing.T) ([]*grpctest.MockTxService, []*grpc.ClientConn) {
				mockSvc1 := &grpctest.MockTxService{
					BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
						return &sdktx.BroadcastTxResponse{TxResponse: &sdk.TxResponse{Code: abci.CodeTypeOK, TxHash: "HASH1"}}, nil
					},
				}
				conn1 := grpctest.StartMockServer(t, mockSvc1)
				return []*grpctest.MockTxService{mockSvc1}, []*grpc.ClientConn{conn1}
			},
			expectError: false,
		},
		{ // Secondary Success
			setupMocks: func(t *testing.T) ([]*grpctest.MockTxService, []*grpc.ClientConn) {
				mockSvc1 := &grpctest.MockTxService{ // Primary fails after delay
					BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
						time.Sleep(1 * time.Second)
						return nil, errMock1
					}}
				mockSvc2 := &grpctest.MockTxService{ // Secondary succeeds quickly
					BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
						return &sdktx.BroadcastTxResponse{TxResponse: &sdk.TxResponse{Code: abci.CodeTypeOK, TxHash: "HASH2"}}, nil
					}}
				mockSvc3 := &grpctest.MockTxService{ // Tertiary should be cancelled
					BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
						select {
						case <-time.After(1 * time.Second):
							return nil, errors.New("mock3 should have been cancelled")
						case <-ctx.Done():
							return nil, ctx.Err()
						}
					}}
				conn1 := grpctest.StartMockServer(t, mockSvc1)
				conn2 := grpctest.StartMockServer(t, mockSvc2)
				conn3 := grpctest.StartMockServer(t, mockSvc3)
				return []*grpctest.MockTxService{
						mockSvc1,
						mockSvc2,
						mockSvc3,
					}, []*grpc.ClientConn{
						conn1,
						conn2,
						conn3,
					}
			},
			expectError: false,
		},
		{ // All Fail
			setupMocks: func(t *testing.T) ([]*grpctest.MockTxService, []*grpc.ClientConn) {
				mockSvc1 := &grpctest.MockTxService{BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
					return nil, errMock1
				}}
				mockSvc2 := &grpctest.MockTxService{BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
					return nil, errMock2
				}}
				mockSvc3 := &grpctest.MockTxService{BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
					return nil, errMock3
				}}
				conn1 := grpctest.StartMockServer(t, mockSvc1)
				conn2 := grpctest.StartMockServer(t, mockSvc2)
				conn3 := grpctest.StartMockServer(t, mockSvc3)
				return []*grpctest.MockTxService{
						mockSvc1,
						mockSvc2,
						mockSvc3,
					}, []*grpc.ClientConn{
						conn1,
						conn2,
						conn3,
					}
			},
			expectError: true,
		},
		{ // Context Deadline
			setupMocks: func(t *testing.T) ([]*grpctest.MockTxService, []*grpc.ClientConn) {
				mockHandler := func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
					select {
					case <-time.After(1 * time.Second):
						return nil, errors.New("mock should have been cancelled")
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				mockSvc1 := &grpctest.MockTxService{BroadcastHandler: mockHandler}
				mockSvc2 := &grpctest.MockTxService{BroadcastHandler: mockHandler}
				mockSvc3 := &grpctest.MockTxService{BroadcastHandler: mockHandler}
				conn1 := grpctest.StartMockServer(t, mockSvc1)
				conn2 := grpctest.StartMockServer(t, mockSvc2)
				conn3 := grpctest.StartMockServer(t, mockSvc3)
				return []*grpctest.MockTxService{
						mockSvc1,
						mockSvc2,
						mockSvc3,
					}, []*grpc.ClientConn{
						conn1,
						conn2,
						conn3,
					}
			},
			expectError: true,
		},
		{ // Less Than Three Conns (Success)
			setupMocks: func(t *testing.T) ([]*grpctest.MockTxService, []*grpc.ClientConn) {
				mockSvc1 := &grpctest.MockTxService{BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
					return nil, errMock1
				}}
				mockSvc2 := &grpctest.MockTxService{BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
					return &sdktx.BroadcastTxResponse{TxResponse: &sdk.TxResponse{Code: abci.CodeTypeOK, TxHash: "HASH_LT3"}}, nil
				}}
				conn1 := grpctest.StartMockServer(t, mockSvc1)
				conn2 := grpctest.StartMockServer(t, mockSvc2)
				return []*grpctest.MockTxService{
						mockSvc1,
						mockSvc2,
					}, []*grpc.ClientConn{
						conn1,
						conn2,
					}
			},
			expectError: false,
		},
		{ // Non-Zero Code Failure
			setupMocks: func(t *testing.T) ([]*grpctest.MockTxService, []*grpc.ClientConn) {
				mockSvc1 := &grpctest.MockTxService{BroadcastHandler: func(ctx context.Context, req *sdktx.BroadcastTxRequest) (*sdktx.BroadcastTxResponse, error) {
					resp := &sdk.TxResponse{Code: 5, TxHash: "HASH_FAIL", RawLog: errInsufficientFunds.Error()}
					return &sdktx.BroadcastTxResponse{TxResponse: resp}, nil
				}}
				conn1 := grpctest.StartMockServer(t, mockSvc1)
				return []*grpctest.MockTxService{mockSvc1}, []*grpc.ClientConn{conn1}
			},
			expectError: true,
		},
	}

	for i, tc := range testCases {
		name := fmt.Sprintf("BroadcastTestCase%d", i) // Simple naming
		t.Run(name, func(t *testing.T) {
			_, conns := tc.setupMocks(t)
			require.NotEmpty(t, conns, "Need at least one connection for broadcast test client")

			primaryConn := conns[0]
			otherConns := conns[1:]

			// Seed a new signer with the suite's default account to avoid querying auth service on mock servers
			origSigner := suite.txClient.Signer()
			origAcc := origSigner.Account(suite.txClient.DefaultAccountName()).Copy()
			signer, err := user.NewSigner(suite.ctx.Keyring, suite.encCfg.TxConfig, origSigner.ChainID(), origAcc)
			require.NoError(t, err)
			tempTxClient, err := user.NewTxClient(
				suite.encCfg.Codec,
				signer,
				primaryConn,
				suite.encCfg.InterfaceRegistry,
				user.WithAdditionalCoreEndpoints(otherConns),
			)
			require.NoError(t, err, "Failed to create temporary TxClient for test case %d", i)

			var ctx context.Context
			var cancel context.CancelFunc
			if name == "BroadcastTestCase3" { // Specifically target the "Context Deadline" case
				ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond) // Short timeout for deadline test
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second) // General timeout
			}
			defer cancel()

			resp, err := tempTxClient.BroadcastTx(ctx, []sdk.Msg{defaultMsg}, defaultOpts...)

			if !tc.expectError {
				require.NoError(t, err, "Expected success, but got error: %v", err)
				require.NotNil(t, resp, "Expected non-nil response on success")
				require.Equal(t, abci.CodeTypeOK, resp.Code, "Expected CodeTypeOK on success")
			} else {
				require.Error(t, err, "Expected an error, but got nil")
			}
		})
	}
}
