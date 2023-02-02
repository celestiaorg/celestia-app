package app_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
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
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
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
	acc := "test"
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(acc)
	signer := blobtypes.NewKeyringSigner(kr, acc, testutil.ChainID)

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
		btxs := blobfactory.ManyMultiBlobTxSameSigner(
			t,
			encCfg.TxConfig.TxEncoder(),
			signer,
			testfactory.Repeat([]int{1}, tt.singleSharePFBs),
			0,
			1, // use the account number 1 since the first account is taken by the validator
		)
		req := abci.RequestPrepareProposal{
			BlockData: &tmproto.Data{
				Txs: coretypes.Txs(btxs).ToSliceOfBytes(),
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
	accnts := testfactory.GenerateAccounts(numBlobTxs + numNormalTxs)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(accnts...)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	infos := queryAccountInfo(testApp, accnts, kr)

	blobTxs := blobfactory.ManyMultiBlobTx(
		t,
		encCfg.TxConfig.TxEncoder(),
		kr,
		testutil.ChainID,
		accnts[:numBlobTxs],
		infos[:numBlobTxs],
		testfactory.Repeat([]*tmproto.Blob{
			{
				NamespaceId:  namespace.RandomBlobNamespace(),
				Data:         []byte{1},
				ShareVersion: uint32(appconsts.DefaultShareVersion),
			},
		}, numBlobTxs),
	)

	normalTxs := testutil.SendTxsWithAccounts(
		t,
		testApp,
		encCfg.TxConfig.TxEncoder(),
		kr,
		1000,
		accnts[0],
		accnts[numBlobTxs:],
		"",
	)
	txs := append(blobTxs, coretypes.Txs(normalTxs).ToSliceOfBytes()...)

	resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
		BlockData: &tmproto.Data{
			Txs: txs,
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

func TestPrepareProposalFiltering(t *testing.T) {
	encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(6)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)

	// create 3 single blob blobTxs that are signed with valid account numbers
	// and sequences
	blobTxs := blobfactory.ManyMultiBlobTx(
		t,
		encConf.TxConfig.TxEncoder(),
		kr,
		testutil.ChainID,
		accounts[:3],
		infos[:3],
		blobfactory.NestedBlobs(
			t,
			[][]byte{
				namespace.RandomBlobNamespace(),
				namespace.RandomBlobNamespace(),
				namespace.RandomBlobNamespace(),
			},
			[][]int{{100}, {1000}, {420}},
		),
	)

	// create 3 MsgSend transactions that are signed with valid account numbers
	// and sequences
	sendTxs := coretypes.Txs(testutil.SendTxsWithAccounts(
		t,
		testApp,
		encConf.TxConfig.TxEncoder(),
		kr,
		1000,
		accounts[0],
		accounts[len(accounts)-3:],
		"",
	)).ToSliceOfBytes()

	validTxs := func() [][]byte {
		txs := make([][]byte, 0, len(sendTxs)+len(blobTxs))
		txs = append(txs, blobTxs...)
		txs = append(txs, sendTxs...)
		return txs
	}

	// create 3 MsgSend transactions that are using the same sequence as the
	// first three blob transactions above
	duplicateSeqSendTxs := coretypes.Txs(testutil.SendTxsWithAccounts(
		t,
		testApp,
		encConf.TxConfig.TxEncoder(),
		kr,
		1000,
		accounts[0],
		accounts[:3],
		"",
	)).ToSliceOfBytes()

	// create a transaction with an account that doesn't exist. This will cause the increment nonce
	nilAccount := "carmon san diego"
	_, _, err := kr.NewMnemonic(nilAccount, keyring.English, "", "", hd.Secp256k1)
	require.NoError(t, err)
	noAccountTx := []byte(testutil.SendTxWithManualSequence(t, encConf.TxConfig.TxEncoder(), kr, nilAccount, accounts[0], 1000, "", 0, 6))

	type test struct {
		name      string
		txs       func() [][]byte
		prunedTxs [][]byte
	}

	tests := []test{
		{
			name:      "all valid txs, none are pruned",
			txs:       func() [][]byte { return validTxs() },
			prunedTxs: [][]byte{},
		},
		{
			// even though duplicateSeqSendTxs are getting appended to the end of the
			// block, and we do not check the signatures of the standard txs,
			// the blob txs still get pruned because we are separating the
			// normal and blob txs, and checking/executing the normal txs first.
			name: "duplicate sequence appended to the end of the block",
			txs: func() [][]byte {
				return append(validTxs(), duplicateSeqSendTxs...)
			},
			prunedTxs: blobTxs,
		},
		{
			name: "duplicate sequence txs",
			txs: func() [][]byte {
				txs := make([][]byte, 0, len(sendTxs)+len(blobTxs)+len(duplicateSeqSendTxs))
				// these should increment the nonce of the accounts that are
				// signing the blobtxs, which should make those signatures
				// invalid.
				txs = append(txs, duplicateSeqSendTxs...)
				txs = append(txs, blobTxs...)
				txs = append(txs, sendTxs...)
				return txs
			},
			prunedTxs: blobTxs,
		},
		{
			name: "nil account panic catch",
			txs: func() [][]byte {
				return [][]byte{noAccountTx}
			},
			prunedTxs: [][]byte{noAccountTx},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
				BlockData: &tmproto.Data{Txs: tt.txs()},
			})
			// check that we have the expected number of transactions
			require.Equal(t, len(tt.txs())-len(tt.prunedTxs), len(resp.BlockData.Txs))
			// check the the expected txs were removed
			for _, ptx := range tt.prunedTxs {
				assert.NotContains(t, resp.BlockData.Txs, ptx)
			}
		})
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
