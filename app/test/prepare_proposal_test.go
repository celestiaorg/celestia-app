package app_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
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
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accnts := testfactory.GenerateAccounts(6)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(accnts...)
	infos := queryAccountInfo(testApp, accnts, kr)

	type test struct {
		input         abci.RequestPrepareProposal
		expectedBlobs []tmproto.Blob
		expectedTxs   int
	}

	blobTxs := blobfactory.ManyMultiBlobTx(
		t,
		encCfg.TxConfig.TxEncoder(),
		kr,
		testutil.ChainID,
		accnts[:3],
		infos[:3],
		[][]*tmproto.Blob{
			{
				{
					NamespaceId: []byte{1, 1, 1, 1, 1, 1, 1, 1},
					Data:        tmrand.Bytes(100),
				},
			},
			{
				{
					NamespaceId: []byte{3, 3, 3, 3, 3, 3, 3, 3},
					Data:        tmrand.Bytes(1000),
				},
			},
			{
				{
					NamespaceId: []byte{2, 2, 2, 2, 2, 2, 2, 2},
					Data:        tmrand.Bytes(420),
				},
			},
		},
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
					Txs: blobTxs,
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

func queryAccountInfo(capp *app.App, accs []string, kr keyring.Keyring) []blobfactory.AccountInfo {
	infos := make([]blobfactory.AccountInfo, len(accs))
	for i, acc := range accs {
		addr := getAddress(acc, kr)
		accI := testutil.DirectQueryAccount(capp, addr)
		infos[i] = blobfactory.AccountInfo{
			AccountNum: accI.GetAccountNumber(),
			Sequence:   accI.GetSequence(),
		}
	}
	return infos
}
