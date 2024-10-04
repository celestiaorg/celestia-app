package app_test

import (
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	v3consts "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"

	tmrand "github.com/tendermint/tendermint/libs/rand"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	"github.com/celestiaorg/go-square/v2/share"
)

func TestPrepareProposalPutsPFBsAtEnd(t *testing.T) {
	numBlobTxs, numNormalTxs := 3, 3
	accnts := testfactory.GenerateAccounts(numBlobTxs + numNormalTxs)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accnts...)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	infos := queryAccountInfo(testApp, accnts, kr)

	protoBlob, err := share.NewBlob(share.RandomBlobNamespace(), []byte{1}, appconsts.DefaultShareVersion, nil)
	require.NoError(t, err)
	blobTxs := blobfactory.ManyMultiBlobTx(
		t,
		encCfg.TxConfig,
		kr,
		testutil.ChainID,
		accnts[:numBlobTxs],
		infos[:numBlobTxs],
		testfactory.Repeat([]*share.Blob{protoBlob}, numBlobTxs),
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
	noAccountTx := []byte(testutil.SendTxWithManualSequence(t, encConf.TxConfig, kr, nilAccount, accounts[0], 1000, "", 0, 6))

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
			// check that the expected txs were removed
			for _, ptx := range tt.prunedTxs {
				require.NotContains(t, resp.BlockData.Txs, ptx)
			}
		})
	}
}

func TestPrepareProposalCappingNumberOfTransactions(t *testing.T) {
	// creating a big number of accounts so that every account
	// only creates a single transaction. This is for transactions
	// to be skipped without worrying about the sequence number being
	// sequential.
	numberOfAccounts := 8000
	accounts := make([]string, 0, numberOfAccounts)
	for i := 0; i < numberOfAccounts; i++ {
		accounts = append(accounts, fmt.Sprintf("account%d", i))
	}
	consensusParams := app.DefaultConsensusParams()
	consensusParams.Block.MaxBytes = 128
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(consensusParams, accounts...)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	addrs := make([]sdk.AccAddress, 0, numberOfAccounts)
	accs := make([]types.AccountI, 0, numberOfAccounts)
	signers := make([]*user.Signer, 0, numberOfAccounts)
	for index, account := range accounts {
		addr := testfactory.GetAddress(kr, account)
		addrs = append(addrs, addr)
		acc := testutil.DirectQueryAccount(testApp, addrs[index])
		accs = append(accs, acc)
		signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, appconsts.LatestVersion, user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()))
		require.NoError(t, err)
		signers = append(signers, signer)
	}

	numberOfPFBs := v3consts.PFBTransactionCap + 500
	pfbTxs := make([][]byte, 0, numberOfPFBs)
	randomBytes := make([]byte, 2000)
	_, err := rand.Read(randomBytes)
	require.NoError(t, err)
	accountIndex := 0
	for i := 0; i < numberOfPFBs; i++ {
		blob, err := share.NewBlob(share.RandomNamespace(), randomBytes, 1, accs[accountIndex].GetAddress().Bytes())
		require.NoError(t, err)
		tx, _, err := signers[accountIndex].CreatePayForBlobs(accounts[accountIndex], []*share.Blob{blob}, user.SetGasLimit(2549760000), user.SetFee(10000))
		require.NoError(t, err)
		pfbTxs = append(pfbTxs, tx)
		accountIndex++
	}

	numberOfMsgSends := v3consts.MsgSendTransactionCap + 500
	msgSendTxs := make([][]byte, 0, numberOfMsgSends)
	for i := 0; i < numberOfMsgSends; i++ {
		msg := banktypes.NewMsgSend(
			addrs[accountIndex],
			testnode.RandomAddress().(sdk.AccAddress),
			sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10)),
		)
		rawTx, err := signers[accountIndex].CreateTx([]sdk.Msg{msg}, user.SetGasLimit(1000000), user.SetFee(10))
		require.NoError(t, err)
		msgSendTxs = append(msgSendTxs, rawTx)
		accountIndex++
	}

	testCases := []struct {
		name                 string
		inputTransactions    [][]byte
		expectedTransactions [][]byte
	}{
		{
			name:                 "capping only PFB transactions",
			inputTransactions:    pfbTxs[:v3consts.PFBTransactionCap+50],
			expectedTransactions: pfbTxs[:v3consts.PFBTransactionCap],
		},
		{
			name:                 "capping only msg send transactions",
			inputTransactions:    msgSendTxs[:v3consts.MsgSendTransactionCap+50],
			expectedTransactions: msgSendTxs[:v3consts.MsgSendTransactionCap],
		},
		{
			name: "capping msg send after pfb transactions",
			inputTransactions: func() [][]byte {
				input := make([][]byte, 0, len(msgSendTxs)+100)
				input = append(input, pfbTxs[:100]...)
				input = append(input, msgSendTxs...)
				return input
			}(),
			expectedTransactions: func() [][]byte {
				expected := make([][]byte, 0, v3consts.MsgSendTransactionCap+100)
				expected = append(expected, msgSendTxs[:v3consts.MsgSendTransactionCap]...)
				expected = append(expected, pfbTxs[:100]...)
				return expected
			}(),
		},
		{
			name: "capping pfb after msg send transactions",
			inputTransactions: func() [][]byte {
				input := make([][]byte, 0, len(pfbTxs)+100)
				input = append(input, msgSendTxs[:100]...)
				input = append(input, pfbTxs...)
				return input
			}(),
			expectedTransactions: func() [][]byte {
				expected := make([][]byte, 0, v3consts.PFBTransactionCap+100)
				expected = append(expected, msgSendTxs[:100]...)
				expected = append(expected, pfbTxs[:v3consts.PFBTransactionCap]...)
				return expected
			}(),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
				BlockData: &tmproto.Data{
					Txs: testCase.inputTransactions,
				},
				ChainId: testApp.GetChainID(),
				Height:  10,
			})
			assert.Equal(t, testCase.expectedTransactions, resp.BlockData.Txs)
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
