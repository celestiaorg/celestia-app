//go:build bench_gas_estimation

package benchmarks_test

import (
	"context"
	"testing"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/app/grpc/gasestimation"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/rpc/client"
	rpctypes "github.com/tendermint/tendermint/rpc/core/types"
	"github.com/tendermint/tendermint/types"
)

func BenchmarkGasPriceEstimation(b *testing.B) {
	encfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer, err := user.NewSigner(kr, encfg.TxConfig, testutil.ChainID, appconsts.LatestVersion, user.NewAccount(accounts[0], infos[0].AccountNum, infos[0].Sequence))
	require.NoError(b, err)

	// 8 mb block
	blockSizeInBytes := 128 * 128 * share.ContinuationSparseShareContentSize

	benchmarks := []struct {
		name                 string
		pfbSize              int
		numberOfTransactions int
	}{
		{
			name:                 "10kb PFBs filling 120% of a block",
			pfbSize:              10_000,
			numberOfTransactions: (blockSizeInBytes * 120 / 100) / 10_000,
		},
		{
			name:                 "100kb PFBs filling 120% of a block",
			pfbSize:              100_000,
			numberOfTransactions: (blockSizeInBytes * 120 / 100) / 100_000,
		},
		{
			name:                 "1mb PFBs filling 120% of a block",
			pfbSize:              1_000_000,
			numberOfTransactions: (blockSizeInBytes * 120 / 100) / 1_000_000,
		},
		{
			name:                 "2mb PFBs filling 120% of a block",
			pfbSize:              2_000_000,
			numberOfTransactions: (blockSizeInBytes * 120 / 100) / 2_000_000,
		},
	}

	for _, bench := range benchmarks {
		b.Run(bench.name, func(b *testing.B) {
			txs := generateRandomPFBs(b, signer, accounts[0], bench.numberOfTransactions, bench.pfbSize)
			mempool := benchMempool{txs: txs}
			gasEstimationServer := gasestimation.NewGasEstimatorServer(
				mempool,
				encfg.TxConfig.TxDecoder(),
				func() (uint64, error) { return 128 * 128 * share.ContinuationSparseShareContentSize, nil },
				func(txBytes []byte) (sdk.GasInfo, *sdk.Result, error) { return sdk.GasInfo{}, nil, nil },
			)
			for i := 0; i < b.N; i++ {
				_, err := gasEstimationServer.EstimateGasPrice(context.Background(), &gasestimation.EstimateGasPriceRequest{})
				require.NoError(b, err)
			}
		})
	}
}

func generateRandomPFBs(b *testing.B, signer *user.Signer, account string, numberOfTransactions int, blobSize int) []types.Tx {
	rand := tmrand.NewRand()
	gasLimit := blobtypes.DefaultEstimateGas([]uint32{uint32(blobSize)})
	blobs := blobfactory.ManyBlobs(rand, []share.Namespace{share.RandomBlobNamespace()}, []int{blobSize})
	txs := make([]types.Tx, numberOfTransactions)
	bTxFee := rand.Uint64() % 10000
	for i := 0; i < numberOfTransactions; i++ {
		bTx, _, err := signer.CreatePayForBlobs(account, blobs,
			user.SetFee(bTxFee),
			user.SetGasLimit(gasLimit))
		require.NoError(b, err)
		txs[i] = bTx
	}
	return txs
}

var _ client.MempoolClient = &benchMempool{}

type benchMempool struct {
	txs []types.Tx
}

func (b benchMempool) UnconfirmedTxs(ctx context.Context, limit *int) (*rpctypes.ResultUnconfirmedTxs, error) {
	return &rpctypes.ResultUnconfirmedTxs{Txs: b.txs}, nil
}

func (b benchMempool) NumUnconfirmedTxs(ctx context.Context) (*rpctypes.ResultUnconfirmedTxs, error) {
	return nil, nil
}

func (b benchMempool) CheckTx(ctx context.Context, tx types.Tx) (*rpctypes.ResultCheckTx, error) {
	return nil, nil
}

func queryAccountInfo(capp *app.App, accs []string, kr keyring.Keyring) []blobfactory.AccountInfo {
	infos := make([]blobfactory.AccountInfo, len(accs))
	for i, acc := range accs {
		addr := testfactory.GetAddress(kr, acc)
		accI := testutil.DirectQueryAccount(capp, addr)
		infos[i] = blobfactory.AccountInfo{
			AccountNum: accI.GetAccountNumber(),
			Sequence:   accI.GetSequence(),
		}
	}
	return infos
}
