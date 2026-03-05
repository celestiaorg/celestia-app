package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v8/test/util"
	"github.com/celestiaorg/celestia-app/v8/test/util/random"
	"github.com/celestiaorg/celestia-app/v8/test/util/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/v8/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/proto/tendermint/version"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBlobOrderingWithinNamespaceByPriority verifies that when blob transactions
// are provided to PrepareProposal in priority order (as the CAT mempool would
// deliver them), the resulting proposal preserves that ordering. Each account
// submits a single PFB to the same namespace with a different gas price.
//
// Note: This tests ordering *preservation* by PrepareProposal, not ordering
// *enforcement*. The mempool is responsible for delivering transactions in
// priority order. See the CAT mempool priority ordering tests in celestia-core
// for mempool-level ordering guarantees.
//
// Addresses https://github.com/celestiaorg/celestia-app/issues/3164
func TestBlobOrderingWithinNamespaceByPriority(t *testing.T) {
	// Create accounts: one per blob transaction so each has an independent sequence.
	numAccounts := 4
	accounts := testfactory.GenerateAccounts(numAccounts)
	encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)

	// All blobs use the same namespace.
	ns := share.RandomBlobNamespace()
	blobSize := 200

	// Define gas prices in descending order (highest priority first).
	// This simulates the order a priority-aware mempool would provide.
	gasPrices := []float64{10.0, 5.0, 2.0, 1.0}

	// Create one blob tx per account, all using the same namespace but different gas prices.
	txs := make([][]byte, numAccounts)
	for i, account := range accounts {
		addr := testfactory.GetAddress(kr, account)
		acc := testutil.DirectQueryAccount(testApp, addr)
		signer, err := user.NewSigner(
			kr, encConf.TxConfig, testutil.ChainID,
			user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()),
		)
		require.NoError(t, err)

		blob, err := blobtypes.NewV0Blob(ns, random.Bytes(blobSize))
		require.NoError(t, err)

		msg, err := blobtypes.NewMsgPayForBlobs(addr.String(), appconsts.Version, blob)
		require.NoError(t, err)
		gasLimit := blobtypes.DefaultEstimateGas(msg)
		txBytes, _, err := signer.CreatePayForBlobs(
			account,
			[]*share.Blob{blob},
			user.SetGasLimitAndGasPrice(gasLimit, gasPrices[i]),
		)
		require.NoError(t, err)
		txs[i] = txBytes
	}

	height := testApp.LastBlockHeight() + 1
	blockTime := time.Now()

	// Feed transactions to PrepareProposal in priority order (highest gas price first).
	prepareResp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
		Txs:    txs,
		Height: height,
		Time:   blockTime,
	})
	require.NoError(t, err)

	// Extract the blob transactions from the response.
	var blobTxGasPrices []float64
	for _, txBytes := range prepareResp.Txs {
		bTx, isBlobTx := coretypes.UnmarshalBlobTx(coretypes.Tx(txBytes))
		if !isBlobTx {
			continue
		}
		gasPrice := extractGasPrice(t, encConf, bTx.Tx)
		blobTxGasPrices = append(blobTxGasPrices, gasPrice)
	}

	require.Len(t, blobTxGasPrices, numAccounts, "all blob txs should be included")

	// Verify that blob transactions are in descending gas price order (highest priority first).
	for i := 0; i < len(blobTxGasPrices)-1; i++ {
		assert.Greaterf(t, blobTxGasPrices[i], blobTxGasPrices[i+1],
			"blob tx at index %d (gas price %.4f) should have higher priority than blob tx at index %d (gas price %.4f)",
			i, blobTxGasPrices[i], i+1, blobTxGasPrices[i+1],
		)
	}

	// Verify that ProcessProposal accepts the proposal.
	processResp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
		Header: &cmtproto.Header{
			Version: version.Consensus{
				Block: 1,
				App:   3,
			},
			ChainID:  testutil.ChainID,
			Height:   height,
			Time:     blockTime,
			DataHash: prepareResp.DataRootHash,
		},
		Height:       height,
		Txs:          prepareResp.Txs,
		SquareSize:   prepareResp.SquareSize,
		DataRootHash: prepareResp.DataRootHash,
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processResp.Status)
}

// TestBlobReorderingWithinNamespaceRejected verifies that if blobs within the
// same namespace are reordered (so they no longer match the priority ordering),
// ProcessProposal rejects the proposal because the data root will not match.
func TestBlobReorderingWithinNamespaceRejected(t *testing.T) {
	numAccounts := 2
	accounts := testfactory.GenerateAccounts(numAccounts)
	encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)

	ns := share.RandomBlobNamespace()
	blobSize := 200
	gasPrices := []float64{10.0, 1.0}

	txs := make([][]byte, numAccounts)
	for i, account := range accounts {
		addr := testfactory.GetAddress(kr, account)
		acc := testutil.DirectQueryAccount(testApp, addr)
		signer, err := user.NewSigner(
			kr, encConf.TxConfig, testutil.ChainID,
			user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()),
		)
		require.NoError(t, err)

		blob, err := blobtypes.NewV0Blob(ns, random.Bytes(blobSize))
		require.NoError(t, err)

		msg, err := blobtypes.NewMsgPayForBlobs(addr.String(), appconsts.Version, blob)
		require.NoError(t, err)
		gasLimit := blobtypes.DefaultEstimateGas(msg)
		txBytes, _, err := signer.CreatePayForBlobs(
			account,
			[]*share.Blob{blob},
			user.SetGasLimitAndGasPrice(gasLimit, gasPrices[i]),
		)
		require.NoError(t, err)
		txs[i] = txBytes
	}

	height := testApp.LastBlockHeight() + 1
	blockTime := time.Now()

	// First, get a valid proposal.
	prepareResp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
		Txs:    txs,
		Height: height,
		Time:   blockTime,
	})
	require.NoError(t, err)

	// Swap the blob transactions to reverse the priority ordering.
	swappedTxs := make([][]byte, len(prepareResp.Txs))
	copy(swappedTxs, prepareResp.Txs)
	blobIdxs := []int{}
	for i, txBytes := range swappedTxs {
		if _, isBlobTx := coretypes.UnmarshalBlobTx(coretypes.Tx(txBytes)); isBlobTx {
			blobIdxs = append(blobIdxs, i)
		}
	}
	require.Len(t, blobIdxs, 2, "expected exactly 2 blob txs")
	swappedTxs[blobIdxs[0]], swappedTxs[blobIdxs[1]] = swappedTxs[blobIdxs[1]], swappedTxs[blobIdxs[0]]

	// ProcessProposal should reject the proposal because the data root won't
	// match when blob transactions are reordered.
	processResp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
		Header: &cmtproto.Header{
			Version: version.Consensus{
				Block: 1,
				App:   3,
			},
			ChainID:  testutil.ChainID,
			Height:   height,
			Time:     blockTime,
			DataHash: prepareResp.DataRootHash,
		},
		Height:       height,
		Txs:          swappedTxs,
		SquareSize:   prepareResp.SquareSize,
		DataRootHash: prepareResp.DataRootHash,
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, processResp.Status)
}

// extractGasPrice computes the gas price (fee / gas) for a raw transaction.
func extractGasPrice(t *testing.T, encConf encoding.Config, txBytes []byte) float64 {
	t.Helper()
	sdkTx, err := encConf.TxConfig.TxDecoder()(txBytes)
	require.NoError(t, err)
	feeTx := sdkTx.(sdk.FeeTx)
	fee := feeTx.GetFee().AmountOf(appconsts.BondDenom).Uint64()
	gas := feeTx.GetGas()
	return float64(fee) / float64(gas)
}
