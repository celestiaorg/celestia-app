package app_test

import (
	"testing"
	"time"

	tmrand "github.com/tendermint/tendermint/libs/rand"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testfactory"
	"github.com/celestiaorg/go-square/blob"
	appns "github.com/celestiaorg/go-square/namespace"
)

func TestPrepareProposalPutsPFBsAtEnd(t *testing.T) {
	numBlobTxs, numNormalTxs := 3, 3
	accnts := testfactory.GenerateAccounts(numBlobTxs + numNormalTxs)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accnts...)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	infos := queryAccountInfo(testApp, accnts, kr)

	blobTxs := blobfactory.ManyMultiBlobTx(
		t,
		encCfg.TxConfig,
		kr,
		testutil.ChainID,
		accnts[:numBlobTxs],
		infos[:numBlobTxs],
		testfactory.Repeat([]*blob.Blob{
			blob.New(appns.RandomBlobNamespace(), []byte{1}, appconsts.DefaultShareVersion),
		}, numBlobTxs),
		app.DefaultConsensusParams().Version.AppVersion,
	)

	normalTxs := testutil.SendTxsWithAccounts(
		t,
		testApp,
		encCfg.TxConfig,
		kr,
		1000,
		accnts[0],
		accnts[numBlobTxs:],
		testutil.ChainID,
	)
	txs := blobTxs
	txs = append(txs, coretypes.Txs(normalTxs).ToSliceOfBytes()...)

	height := testApp.LastBlockHeight() + 1
	blockTime := time.Now()

	resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
		BlockData: &tmproto.Data{
			Txs: txs,
		},
		ChainId: testutil.ChainID,
		Height:  height,
		Time:    blockTime,
	})
	require.Len(t, resp.BlockData.Txs, numBlobTxs+numNormalTxs)
	for idx, txBytes := range resp.BlockData.Txs {
		_, isBlobTx := coretypes.UnmarshalBlobTx(coretypes.Tx(txBytes))
		if idx < numNormalTxs {
			require.False(t, isBlobTx)
		} else {
			require.True(t, isBlobTx)
		}
	}
}

func TestPrepareProposalFiltering(t *testing.T) {
	encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(6)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)

	// create 3 single blob blobTxs that are signed with valid account numbers
	// and sequences
	blobTxs := blobfactory.ManyMultiBlobTx(
		t,
		encConf.TxConfig,
		kr,
		testutil.ChainID,
		accounts[:3],
		infos[:3],
		blobfactory.NestedBlobs(
			t,
			testfactory.RandomBlobNamespaces(tmrand.NewRand(), 3),
			[][]int{{100}, {1000}, {420}},
		),
		app.DefaultConsensusParams().Version.AppVersion,
	)

	// create 3 MsgSend transactions that are signed with valid account numbers
	// and sequences
	sendTxs := coretypes.Txs(testutil.SendTxsWithAccounts(
		t,
		testApp,
		encConf.TxConfig,
		kr,
		1000,
		accounts[0],
		accounts[len(accounts)-3:],
		testutil.ChainID,
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
		encConf.TxConfig,
		kr,
		1000,
		accounts[0],
		accounts[:3],
		testutil.ChainID,
	)).ToSliceOfBytes()

	// create a transaction with an account that doesn't exist. This will cause the increment nonce
	nilAccount := "carmon san diego"
	_, _, err := kr.NewMnemonic(nilAccount, keyring.English, "", "", hd.Secp256k1)
	require.NoError(t, err)
	noAccountTx := []byte(testutil.SendTxWithManualSequence(t, encConf.TxConfig, kr, nilAccount, accounts[0], 1000, "", 0, app.DefaultConsensusParams().Version.AppVersion, 6))

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
			height := testApp.LastBlockHeight() + 1
			blockTime := time.Now()

			resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
				BlockData: &tmproto.Data{Txs: tt.txs()},
				ChainId:   testutil.ChainID,
				Height:    height,
				Time:      blockTime,
			})
			// check that we have the expected number of transactions
			require.Equal(t, len(tt.txs())-len(tt.prunedTxs), len(resp.BlockData.Txs))
			// check the the expected txs were removed
			for _, ptx := range tt.prunedTxs {
				require.NotContains(t, resp.BlockData.Txs, ptx)
			}
		})
	}
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
