package gasestimation_test

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/app/grpc/gasestimation"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/celestia-app/v6/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v6/test/util/random"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/stretchr/testify/require"
)

func TestGasEstimatorE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestGasEstimatorE2E in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Create test accounts
	accounts := testfactory.GenerateAccounts(2)

	// Set gov max square size to 2
	blobParams := blobtypes.DefaultParams()
	blobParams.GovMaxSquareSize = 2

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	// Create test node configuration
	cfg := testnode.DefaultConfig().
		WithFundedAccounts(accounts...).
		WithTimeoutCommit(100 * time.Millisecond).
		WithGenesis(
			genesis.NewDefaultGenesis().
				WithValidators(genesis.NewDefaultValidator(testnode.DefaultValidatorAccountName)).
				WithModifiers(
					genesis.SetBlobParams(enc.Codec, blobParams),
				),
		)

	// Start the test network
	cctx, _, _ := testnode.NewNetwork(t, cfg)

	estimatorClient := gasestimation.NewGasEstimatorClient(cctx.GRPCClient)
	gasPriceResp, err := estimatorClient.EstimateGasPrice(ctx, &gasestimation.EstimateGasPriceRequest{})
	require.NoError(t, err)
	require.Equal(t, gasPriceResp.EstimatedGasPrice, appconsts.DefaultMinGasPrice)

	// Setup transaction client
	txClient, err := user.SetupTxClient(ctx, cctx.Keyring, cctx.GRPCClient, enc)
	require.NoError(t, err)

	// Submit a transaction that takes up 2000 bytes with a high fee
	highGasPrice := 0.1
	blobSize := 1200
	data := random.Bytes(blobSize)
	blob, err := share.NewV0Blob(share.RandomBlobNamespace(), data)
	gasLimit := blobtypes.DefaultEstimateGas([]uint32{uint32(blobSize)})
	require.NoError(t, err)

	// Broadcast the transaction with the high fee
	resp, err := txClient.BroadcastPayForBlob(
		ctx,
		[]*share.Blob{blob},
		user.SetGasLimitAndGasPrice(gasLimit, highGasPrice),
	)
	require.NoError(t, err)
	require.NotNil(t, resp)

	gasPriceResp, err = estimatorClient.EstimateGasPrice(ctx, &gasestimation.EstimateGasPriceRequest{})
	require.NoError(t, err)
	require.Greater(t, gasPriceResp.EstimatedGasPrice, highGasPrice)

	_, err = txClient.ConfirmTx(ctx, resp.TxHash)
	require.NoError(t, err)

	gasPriceResp, err = estimatorClient.EstimateGasPrice(ctx, &gasestimation.EstimateGasPriceRequest{})
	require.NoError(t, err)
	require.Equal(t, gasPriceResp.EstimatedGasPrice, appconsts.DefaultMinGasPrice)
}

func TestGasEstimatorE2EWithNetworkMinGasPrice(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestGasEstimatorE2EWithNetworkMinGasPrice in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Create test accounts
	accounts := testfactory.GenerateAccounts(2)

	// Set gov max square size to 2
	blobParams := blobtypes.DefaultParams()
	blobParams.GovMaxSquareSize = 2

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	networkMinGasPrice := 0.123

	// Create test node configuration
	cfg := testnode.DefaultConfig().
		WithFundedAccounts(accounts...).
		WithTimeoutCommit(100 * time.Millisecond).
		WithGenesis(
			genesis.NewDefaultGenesis().
				WithValidators(genesis.NewDefaultValidator(testnode.DefaultValidatorAccountName)).
				WithModifiers(
					genesis.SetBlobParams(enc.Codec, blobParams),
					genesis.SetMinGasPrice(enc.Codec, networkMinGasPrice),
				).
				WithGasPrice(networkMinGasPrice),
		)

	// Start the test network
	cctx, _, _ := testnode.NewNetwork(t, cfg)

	estimatorClient := gasestimation.NewGasEstimatorClient(cctx.GRPCClient)
	gasPriceResp, err := estimatorClient.EstimateGasPrice(ctx, &gasestimation.EstimateGasPriceRequest{})
	require.NoError(t, err)
	require.Equal(t, gasPriceResp.EstimatedGasPrice, networkMinGasPrice)
}
