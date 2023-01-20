package app_test

import (
	"testing"

	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil"
	"github.com/celestiaorg/celestia-app/testutil/blobfactory"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
	"github.com/celestiaorg/celestia-app/x/blob/types"
)

func TestPrepareProposalBlobSorting(t *testing.T) {
	signer := types.GenerateKeyringSigner(t, types.TestAccName)

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	testApp, _ := testutil.SetupTestAppWithGenesisValSet()

	type test struct {
		input         abci.RequestPrepareProposal
		expectedBlobs []tmproto.Blob
		expectedTxs   int
	}

	blobTxs := blobfactory.RandBlobTxsWithNamespacesAndSigner(
		encCfg.TxConfig.TxEncoder(),
		signer,
		[][]byte{
			{1, 1, 1, 1, 1, 1, 1, 1},
			{3, 3, 3, 3, 3, 3, 3, 3},
			{2, 2, 2, 2, 2, 2, 2, 2},
		},
		[]int{100, 1000, 420},
	)
	decodedBlobTxs := make([]tmproto.BlobTx, 0, len(blobTxs))
	for _, rawBtx := range blobTxs {
		btx, isbtx := coretypes.UnmarshalBlobTx(rawBtx)
		if !isbtx {
			panic("unexpected testing error")
		}
		decodedBlobTxs = append(decodedBlobTxs, btx)
	}

	tests := []test{
		{
			input: abci.RequestPrepareProposal{
				BlockData: &tmproto.Data{
					Txs: coretypes.Txs(blobTxs).ToSliceOfBytes(),
				},
			},
			expectedBlobs: []tmproto.Blob{
				{
					NamespaceId: decodedBlobTxs[0].Blobs[0].NamespaceId,
					Data:        decodedBlobTxs[0].Blobs[0].Data,
				},
				{
					NamespaceId: decodedBlobTxs[2].Blobs[0].NamespaceId,
					Data:        decodedBlobTxs[2].Blobs[0].Data,
				},
				{
					NamespaceId: decodedBlobTxs[1].Blobs[0].NamespaceId,
					Data:        decodedBlobTxs[1].Blobs[0].Data,
				},
			},
			expectedTxs: 3,
		},
	}

	for _, tt := range tests {
		res := testApp.PrepareProposal(tt.input)
		assert.Equal(t, tt.expectedBlobs, res.BlockData.Blobs)
		assert.Equal(t, tt.expectedTxs, len(res.BlockData.Txs))

		// verify the signatures of the prepared txs
		sdata, err := signer.GetSignerData()
		require.NoError(t, err)

		dec := encoding.IndexWrapperDecoder(encCfg.TxConfig.TxDecoder())
		for _, tx := range res.BlockData.Txs {
			sTx, err := dec(tx)
			require.NoError(t, err)

			sigTx, ok := sTx.(authsigning.SigVerifiableTx)
			require.True(t, ok)

			sigs, err := sigTx.GetSignaturesV2()
			require.NoError(t, err)
			require.Equal(t, 1, len(sigs))
			sig := sigs[0]

			err = authsigning.VerifySignature(
				sdata.PubKey,
				sdata,
				sig.Data,
				encCfg.TxConfig.SignModeHandler(),
				sTx,
			)
			assert.NoError(t, err)
		}
	}
}

func TestPrepareProposalOverflow(t *testing.T) {
	signer := types.GenerateKeyringSigner(t, types.TestAccName)

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	testApp, _ := testutil.SetupTestAppWithGenesisValSet()

	type test struct {
		name               string
		singleSharePFBs    int
		expectedTxsInBlock int
		expectedSquareSize uint64
	}

	limit := appconsts.TransactionsPerBlockLimit

	tests := []test{
		{
			name:               "one below the limit",
			singleSharePFBs:    limit - 1,
			expectedTxsInBlock: limit - 1,
			expectedSquareSize: appconsts.DefaultMaxSquareSize,
		},
		{
			name:               "exactly the limit",
			singleSharePFBs:    limit,
			expectedTxsInBlock: limit,
			expectedSquareSize: appconsts.DefaultMaxSquareSize,
		},
		{
			name:               "well above the limit",
			singleSharePFBs:    limit + 5000,
			expectedTxsInBlock: limit,
			expectedSquareSize: appconsts.DefaultMaxSquareSize,
		},
	}

	for _, tt := range tests {
		blobTxs := blobfactory.RandBlobTxsWithNamespacesAndSigner(
			encCfg.TxConfig.TxEncoder(),
			signer,
			testfactory.Repeat([]byte{1, 2, 3, 4, 5, 6, 7, 8}, tt.singleSharePFBs),
			testfactory.Repeat(1, tt.singleSharePFBs),
		)
		req := abci.RequestPrepareProposal{
			BlockData: &tmproto.Data{
				Txs: coretypes.Txs(blobTxs).ToSliceOfBytes(),
			},
		}
		res := testApp.PrepareProposal(req)
		assert.Equal(t, tt.expectedSquareSize, res.BlockData.SquareSize, tt.name)
		assert.Equal(t, tt.expectedTxsInBlock, len(res.BlockData.Blobs), tt.name)
		assert.Equal(t, tt.expectedTxsInBlock, len(res.BlockData.Txs), tt.name)
	}
}

func TestPrepareProposalPutsPFBsAtEnd(t *testing.T) {
	numBlobTxs, numNormalTxs := 3, 3
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	blobTxs := blobfactory.RandBlobTxs(encCfg.TxConfig.TxEncoder(), numBlobTxs, 100)
	normalTxs := blobfactory.GenerateManyRawSendTxs(encCfg.TxConfig, numNormalTxs)
	txs := append(blobTxs, normalTxs...)
	testApp, _ := testutil.SetupTestAppWithGenesisValSet()

	resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
		BlockData: &tmproto.Data{
			Txs: coretypes.Txs(txs).ToSliceOfBytes(),
		},
	})
	require.Len(t, resp.BlockData.Txs, numBlobTxs+numNormalTxs)
	for idx, txBytes := range resp.BlockData.Txs {
		_, isWrapper := coretypes.UnmarshalIndexWrapper(coretypes.Tx(txBytes))
		if idx < numNormalTxs {
			require.False(t, isWrapper)
		} else {
			require.True(t, isWrapper)
		}
	}
}
