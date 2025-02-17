package app_test

import (
	"crypto/rand"
	"strings"
	"testing"
	"time"

	blobtypes "github.com/celestiaorg/celestia-app/v4/x/blob/types"
	blobtx "github.com/celestiaorg/go-square/v2/tx"

	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"

	tmrand "cosmossdk.io/math/unsafe"

	abci "github.com/cometbft/cometbft/abci/types"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/go-square/v2/share"
)

func TestPrepareProposalPutsPFBsAtEnd(t *testing.T) {
	numBlobTxs, numNormalTxs := 3, 3
	accnts := testfactory.GenerateAccounts(numBlobTxs + numNormalTxs)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accnts...)
	enc := moduletestutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)
	infos := queryAccountInfo(testApp, accnts, kr)

	protoBlob, err := share.NewBlob(share.RandomBlobNamespace(), []byte{1}, appconsts.DefaultShareVersion, nil)
	require.NoError(t, err)
	blobTxs := blobfactory.ManyMultiBlobTx(
		t,
		enc.TxConfig,
		kr,
		testutil.ChainID,
		accnts[:numBlobTxs],
		infos[:numBlobTxs],
		testfactory.Repeat([]*share.Blob{protoBlob}, numBlobTxs),
	)

	normalTxs := testutil.SendTxsWithAccounts(
		t,
		testApp,
		enc.TxConfig,
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

	resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
		Txs:    txs,
		Height: height,
		Time:   blockTime,
	})
	require.NoError(t, err)
	require.Len(t, resp.Txs, numBlobTxs+numNormalTxs)
	for idx, txBytes := range resp.Txs {
		_, isBlobTx := coretypes.UnmarshalBlobTx(coretypes.Tx(txBytes))
		if idx < numNormalTxs {
			require.False(t, isBlobTx)
		} else {
			require.True(t, isBlobTx)
		}
	}
}

func TestPrepareProposalFiltering(t *testing.T) {
	enc := moduletestutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(6)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)

	// create 3 single blob blobTxs that are signed with valid account numbers
	// and sequences
	blobTxs := blobfactory.ManyMultiBlobTx(
		t,
		enc.TxConfig,
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
		enc.TxConfig,
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
		enc.TxConfig,
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
	noAccountTx := []byte(testutil.SendTxWithManualSequence(t, enc.TxConfig, kr, nilAccount, accounts[0], 1000, "", 0, 6))

	// create a tx that can't be included in a 64 x 64 when accounting for the
	// pfb along with the shares
	tooManyShareBtx := blobfactory.ManyMultiBlobTx(
		t,
		enc.TxConfig,
		kr,
		testutil.ChainID,
		accounts[3:4],
		infos[3:4],
		blobfactory.NestedBlobs(
			t,
			testfactory.RandomBlobNamespaces(tmrand.NewRand(), 4000),
			[][]int{repeat(4000, 1)},
		),
	)[0]

	// memo is 2 MiB resulting in the transaction being over limit
	largeString := strings.Repeat("a", 2*1024*1024)

	// 3 transactions over MaxTxSize limit
	largeTxs := coretypes.Txs(testutil.SendTxsWithAccounts(t, testApp, enc.TxConfig, kr, 1000, accounts[0], accounts[:3], testutil.ChainID, user.SetMemo(largeString))).ToSliceOfBytes()

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
		{
			name: "blob tx with too many shares",
			txs: func() [][]byte {
				return [][]byte{tooManyShareBtx}
			},
			prunedTxs: [][]byte{tooManyShareBtx},
		},
		{
			name: "blobTxs and sendTxs that exceed MaxTxSize limit",
			txs: func() [][]byte {
				return largeTxs // All txs are over MaxTxSize limit
			},
			prunedTxs: largeTxs,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			height := testApp.LastBlockHeight() + 1
			blockTime := time.Now()

			resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
				Txs:    tt.txs(),
				Height: height,
				Time:   blockTime,
			})
			require.NoError(t, err)
			// check that we have the expected number of transactions
			require.Equal(t, len(tt.txs())-len(tt.prunedTxs), len(resp.Txs))
			// check that the expected txs were removed
			for _, ptx := range tt.prunedTxs {
				require.NotContains(t, resp.Txs, ptx)
			}
		})
	}
}

func TestPrepareProposalCappingNumberOfMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping prepare proposal capping number of transactions test in short mode.")
	}
	// creating a big number of accounts so that every account
	// only creates a single transaction. This is for transactions
	// to be skipped without worrying about the sequence number being
	// sequential.
	numberOfAccounts := 8000
	accounts := testnode.GenerateAccounts(numberOfAccounts)
	consensusParams := app.DefaultConsensusParams()
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(consensusParams, 128, accounts...)
	enc := moduletestutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)

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

	numberOfPFBs := appconsts.MaxPFBMessages + 500
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

	multiPFBsPerTxs := make([][]byte, 0, numberOfPFBs)
	numberOfMsgsPerTx := 10
	for i := 0; i < numberOfPFBs; i++ {
		msgs := make([]sdk.Msg, 0)
		blobs := make([]*share.Blob, 0)
		for j := 0; j < numberOfMsgsPerTx; j++ {
			blob, err := share.NewBlob(share.RandomNamespace(), randomBytes, 1, accs[accountIndex].GetAddress().Bytes())
			require.NoError(t, err)
			msg, err := blobtypes.NewMsgPayForBlobs(addrs[accountIndex].String(), appconsts.LatestVersion, blob)
			require.NoError(t, err)
			msgs = append(msgs, msg)
			blobs = append(blobs, blob)
		}
		txBytes, _, err := signers[accountIndex].CreateTx(msgs, user.SetGasLimit(2549760000), user.SetFee(10000))
		require.NoError(t, err)
		blobTx, err := blobtx.MarshalBlobTx(txBytes, blobs...)
		require.NoError(t, err)
		multiPFBsPerTxs = append(multiPFBsPerTxs, blobTx)
		accountIndex++
	}

	numberOfMsgSends := appconsts.MaxNonPFBMessages + 500
	msgSendTxs := make([][]byte, 0, numberOfMsgSends)
	for i := 0; i < numberOfMsgSends; i++ {
		msg := banktypes.NewMsgSend(
			addrs[accountIndex],
			testnode.RandomAddress().(sdk.AccAddress),
			sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10)),
		)
		rawTx, _, err := signers[accountIndex].CreateTx([]sdk.Msg{msg}, user.SetGasLimit(1000000), user.SetFee(10))
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
			inputTransactions:    pfbTxs[:appconsts.MaxPFBMessages+50],
			expectedTransactions: pfbTxs[:appconsts.MaxPFBMessages],
		},
		{
			name:                 "capping only PFB transactions with multiple messages",
			inputTransactions:    multiPFBsPerTxs[:appconsts.MaxPFBMessages],
			expectedTransactions: multiPFBsPerTxs[:appconsts.MaxPFBMessages/numberOfMsgsPerTx],
		},
		{
			name:                 "capping only msg send transactions",
			inputTransactions:    msgSendTxs[:appconsts.MaxNonPFBMessages+50],
			expectedTransactions: msgSendTxs[:appconsts.MaxNonPFBMessages],
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
				expected := make([][]byte, 0, appconsts.MaxNonPFBMessages+100)
				expected = append(expected, msgSendTxs[:appconsts.MaxNonPFBMessages]...)
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
				expected := make([][]byte, 0, appconsts.MaxPFBMessages+100)
				expected = append(expected, msgSendTxs[:100]...)
				expected = append(expected, pfbTxs[:appconsts.MaxPFBMessages]...)
				return expected
			}(),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
				Txs:    testCase.inputTransactions,
				Height: 10,
			})
			require.NoError(t, err)
			assert.Equal(t, testCase.expectedTransactions, resp.Txs)
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

// repeat returns a slice of length n with each element set to val.
func repeat[T any](n int, val T) []T {
	result := make([]T, n)
	for i := range result {
		result[i] = val
	}
	return result
}
