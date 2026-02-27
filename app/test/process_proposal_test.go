package app_test

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/pkg/da"
	"github.com/celestiaorg/celestia-app/v8/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v8/test/util"
	"github.com/celestiaorg/celestia-app/v8/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v8/test/util/random"
	"github.com/celestiaorg/celestia-app/v8/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v8/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v8/x/blob/types"
	"github.com/celestiaorg/go-square/v3"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/celestiaorg/go-square/v3/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestProcessProposal(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(6)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(accounts[0], infos[0].AccountNum, infos[0].Sequence))
	require.NoError(t, err)

	// create 4 single blob blobTxs that are signed with valid account numbers
	// and sequences
	blobTxs := blobfactory.ManyMultiBlobTx(
		t, enc.TxConfig, kr, testutil.ChainID, accounts[:4], infos[:4],
		blobfactory.NestedBlobs(
			t,
			testfactory.RandomBlobNamespaces(random.New(), 4),
			[][]int{{100}, {1000}, {420}, {300}},
		),
	)

	// create 3 MsgSend transactions that are signed with valid account numbers
	// and sequences
	sendTxs := testutil.SendTxsWithAccounts(
		t, testApp, enc.TxConfig, kr, 1000, accounts[0], accounts[len(accounts)-3:], testutil.ChainID,
	)

	// block with all blobs included
	validData := func() *tmproto.Data {
		return &tmproto.Data{
			Txs: blobTxs[:3],
		}
	}

	mixedData := validData()
	mixedData.Txs = append(coretypes.Txs(sendTxs).ToSliceOfBytes(), mixedData.Txs...)

	// create an invalid block by adding an otherwise valid PFB, but an invalid
	// signature since there's no account
	badSigBlobTx := testutil.RandBlobTxsWithManualSequence(
		t, enc.TxConfig, kr, 1000, 1, false, testutil.ChainID, accounts[:1], 1, 1, true,
	)[0]

	blobTxWithInvalidNonce := testutil.RandBlobTxsWithManualSequence(
		t, enc.TxConfig, kr, 1000, 1, false, testutil.ChainID, accounts[:1], 1, 3, false,
	)[0]

	ns1 := share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	data := bytes.Repeat([]byte{1}, 13)

	type test struct {
		name           string
		input          *tmproto.Data
		mutator        func(*tmproto.Data)
		expectedResult abci.ResponseProcessProposal_ProposalStatus
	}

	tests := []test{
		{
			name:           "valid untouched data",
			input:          validData(),
			mutator:        func(_ *tmproto.Data) {},
			expectedResult: abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name:  "removed first blob tx",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				d.Txs = d.Txs[1:]
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "added an extra blob tx",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				d.Txs = append(d.Txs, blobTxs[3])
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "modified a blobTx",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				blobTx, _, err := tx.UnmarshalBlobTx(blobTxs[0])
				require.NoError(t, err)
				newBlob, err := share.NewBlob(ns1, data, share.ShareVersionZero, nil)
				require.NoError(t, err)
				blobTx.Blobs[0] = newBlob
				blobTxBytes, _ := tx.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
				d.Txs[0] = blobTxBytes
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid namespace TxNamespace",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				blobTx, _, err := tx.UnmarshalBlobTx(blobTxs[0])
				require.NoError(t, err)
				newBlob, err := share.NewBlob(share.TxNamespace, data, share.ShareVersionZero, nil)
				require.NoError(t, err)
				blobTx.Blobs[0] = newBlob
				blobTxBytes, _ := tx.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
				d.Txs[0] = blobTxBytes
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid namespace in index wrapper tx",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				index := 4
				transaction, b := blobfactory.IndexWrappedTxWithInvalidNamespace(t, random.New(), signer, uint32(index))
				blobTx, err := tx.MarshalBlobTx(transaction, b)
				require.NoError(t, err)

				// Replace the data with new contents
				d.Txs = [][]byte{blobTx}

				// Erasure code the data to update the data root so this doesn't fail on an incorrect data root.
				d.Hash = calculateNewDataHash(t, d.Txs)
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "swap blobTxs",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				// swapping the order will cause the data root to be different
				d.Txs[0], d.Txs[1], d.Txs[2] = d.Txs[1], d.Txs[2], d.Txs[0]
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "PFB without blobTx",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				btx, _ := coretypes.UnmarshalBlobTx(blobTxs[3])
				d.Txs = append(d.Txs, btx.Tx)
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "incorrectly sorted; send tx after pfb",
			input: mixedData,
			mutator: func(d *tmproto.Data) {
				// swap txs at index 2 and 3 (essentially swapping a PFB with a normal tx)
				d.Txs[3], d.Txs[2] = d.Txs[2], d.Txs[3]
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "included pfb with bad signature",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				d.Txs = append(d.Txs, badSigBlobTx)
				d.Hash = calculateNewDataHash(t, d.Txs)
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "included pfb with incorrect nonce",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				d.Txs = append(d.Txs, blobTxWithInvalidNonce)
				d.Hash = calculateNewDataHash(t, d.Txs)
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "tampered sequence start",
			input: &tmproto.Data{
				Txs: coretypes.Txs(sendTxs).ToSliceOfBytes(),
			},
			mutator: func(d *tmproto.Data) {
				dataSquare, err := square.Construct(d.Txs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
				require.NoError(t, err)

				b := dataSquare[1].ToBytes()
				// flip the sequence start
				b[share.NamespaceSize] ^= 0x01
				updatedShare, err := share.NewShare(b)
				require.NoError(t, err)
				dataSquare[1] = *updatedShare

				eds, err := da.ExtendShares(share.ToBytes(dataSquare))
				require.NoError(t, err)

				dah, err := da.NewDataAvailabilityHeader(eds)
				require.NoError(t, err)
				// replace the hash of the prepare proposal response with the hash of a data
				// square with a tampered sequence start indicator
				d.Hash = dah.Hash()
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "valid v1 authored blob",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				addr := signer.Account(accounts[0]).Address()
				blob, err := share.NewV1Blob(ns1, data, addr)
				require.NoError(t, err)
				rawTx, _, err := signer.CreatePayForBlobs(accounts[0], []*share.Blob{blob}, user.SetGasLimit(100000), user.SetFee(100000))
				require.NoError(t, err)
				d.Txs[0] = rawTx
				d.Hash = calculateNewDataHash(t, d.Txs)
			},
			expectedResult: abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name:  "v1 authored blob with invalid signer",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				addr := signer.Account(accounts[0]).Address()
				falseAddr := testnode.RandomAddress().(sdk.AccAddress)
				blob, err := share.NewV1Blob(ns1, data, falseAddr)
				require.NoError(t, err)
				msg, err := blobtypes.NewMsgPayForBlobs(falseAddr.String(), appconsts.Version, blob)
				require.NoError(t, err)
				msg.Signer = addr.String()

				rawTx, _, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(100000), user.SetFee(100000))
				require.NoError(t, err)

				blobTxBytes, err := tx.MarshalBlobTx(rawTx, blob)
				require.NoError(t, err)
				d.Txs[0] = blobTxBytes
				d.Hash = calculateNewDataHash(t, d.Txs)
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "tx size exceeds max tx size limit",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				maxTxSize := appconsts.MaxTxSize // max tx size for the latest version
				// set the blob size to maxTxSize so that the raw transaction size will exceeds the max tx size limit
				blob, err := share.NewBlob(ns1, bytes.Repeat([]byte{1}, maxTxSize), appconsts.DefaultShareVersion, nil)
				require.NoError(t, err)
				rawTx, _, err := signer.CreatePayForBlobs(accounts[0], []*share.Blob{blob}, user.SetGasLimit(100000), user.SetFee(100000))
				require.NoError(t, err)
				// override the last valid blob tx with large one that exceeds the max tx size limit
				// proposal block should be rejected
				d.Txs[2] = rawTx
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			blockTime, height := time.Now(), testApp.LastBlockHeight()+1

			resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
				Txs:    tc.input.Txs,
				Height: height,
				Time:   blockTime,
			})
			require.NoError(t, err)
			require.Equal(t, len(tc.input.Txs), len(resp.Txs))

			blockData := &tmproto.Data{
				Txs:        resp.Txs,
				Hash:       resp.DataRootHash,
				SquareSize: resp.SquareSize,
			}

			tc.mutator(blockData)

			res, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
				Time:         blockTime,
				Height:       height,
				Txs:          blockData.Txs,
				DataRootHash: blockData.Hash,
				SquareSize:   blockData.SquareSize,
			})
			require.NoError(t, err)
			require.Equal(t, tc.expectedResult, res.Status, fmt.Sprintf("expected %v, got %v", tc.expectedResult, res.Status))
		})
	}
}

func calculateNewDataHash(t *testing.T, txs [][]byte) []byte {
	dataSquare, err := square.Construct(txs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)
	eds, err := da.ExtendShares(share.ToBytes(dataSquare))
	require.NoError(t, err)
	dah, err := da.NewDataAvailabilityHeader(eds)
	require.NoError(t, err)
	return dah.Hash()
}

func TestProcessProposal_ProposalWithInconsistentBlobTxFails(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(2)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(accounts[0], infos[0].AccountNum, infos[0].Sequence))
	require.NoError(t, err)

	ns := share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	blobTxBytes := blobfactory.RandBlobTxsWithNamespacesAndSigner(signer, []share.Namespace{ns}, []int{100})[0]

	blobTx, isBlobTx, err := tx.UnmarshalBlobTx(blobTxBytes)
	require.NoError(t, err)
	require.True(t, isBlobTx)

	// run CheckTx to populate the cache with the original blob hash
	checkTxResp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: blobTxBytes, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, uint32(0), checkTxResp.Code, "CheckTx should pass: %s", checkTxResp.Log)

	t.Run("Proposal with inconsistent blob tx fails", func(t *testing.T) {
		// replace the blob with a different one (same namespace, different data)
		inconsistentBlob, err := share.NewBlob(ns, bytes.Repeat([]byte{0xDE, 0xAD}, 50), share.ShareVersionZero, nil)
		require.NoError(t, err)
		inconsistentBlobTxBytes, err := tx.MarshalBlobTx(blobTx.Tx, inconsistentBlob)
		require.NoError(t, err)

		res, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
			Time:         time.Now(),
			Height:       testApp.LastBlockHeight() + 1,
			Txs:          [][]byte{inconsistentBlobTxBytes},
			DataRootHash: calculateNewDataHash(t, [][]byte{inconsistentBlobTxBytes}),
			SquareSize:   2,
		})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, res.Status, "ProcessProposal should reject inconsistent blob tx")
	})

	t.Run("Original blob proposal succeeds", func(t *testing.T) {
		res, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
			Time:         time.Now(),
			Height:       testApp.LastBlockHeight() + 1,
			Txs:          [][]byte{blobTxBytes},
			DataRootHash: calculateNewDataHash(t, [][]byte{blobTxBytes}), // original data hash
			SquareSize:   2,
		})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, res.Status, "ProcessProposal should accept original blob")
	})
}

func TestProcessProposalCappingNumberOfMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process proposal capping number of messages test in short mode.")
	}

	// Create enough accounts so each sends exactly one tx (avoids sequence collisions).
	numberOfAccounts := 2000
	accounts := testfactory.GenerateAccounts(numberOfAccounts)
	consensusParams := app.DefaultConsensusParams()
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(consensusParams, 128, accounts...)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	addrs := make([]sdk.AccAddress, 0, numberOfAccounts)
	signers := make([]*user.Signer, 0, numberOfAccounts)
	accs := make([]sdk.AccountI, 0, numberOfAccounts)
	for index, account := range accounts {
		addr := testfactory.GetAddress(kr, account)
		addrs = append(addrs, addr)
		acc := testutil.DirectQueryAccount(testApp, addrs[index])
		accs = append(accs, acc)
		signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()))
		require.NoError(t, err)
		signers = append(signers, signer)
	}

	accountIndex := 0

	// Generate MaxNonPFBMessages+1 MsgSend txs.
	numMsgSends := appconsts.MaxNonPFBMessages + 1
	msgSendTxs := make([][]byte, 0, numMsgSends)
	for range numMsgSends {
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

	// Generate MaxPFBMessages+1 PFB blob txs.
	numPFBs := appconsts.MaxPFBMessages + 1
	pfbTxs := make([][]byte, 0, numPFBs)
	randomBytes := make([]byte, 2000)
	_, err := rand.Read(randomBytes)
	require.NoError(t, err)
	for range numPFBs {
		blob, err := share.NewBlob(share.RandomNamespace(), randomBytes, 1, accs[accountIndex].GetAddress().Bytes())
		require.NoError(t, err)
		blobTx, _, err := signers[accountIndex].CreatePayForBlobs(accounts[accountIndex], []*share.Blob{blob}, user.SetGasLimit(2549760000), user.SetFee(10000))
		require.NoError(t, err)
		pfbTxs = append(pfbTxs, blobTx)
		accountIndex++
	}

	type testCase struct {
		name           string
		txs            [][]byte
		expectedResult abci.ResponseProcessProposal_ProposalStatus
	}

	testCases := []testCase{
		{
			name:           "reject block exceeding MaxNonPFBMessages",
			txs:            msgSendTxs[:appconsts.MaxNonPFBMessages+1],
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:           "reject block exceeding MaxPFBMessages",
			txs:            pfbTxs[:appconsts.MaxPFBMessages+1],
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:           "accept block at exactly MaxNonPFBMessages",
			txs:            msgSendTxs[:appconsts.MaxNonPFBMessages],
			expectedResult: abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name:           "accept block at exactly MaxPFBMessages",
			txs:            pfbTxs[:appconsts.MaxPFBMessages],
			expectedResult: abci.ResponseProcessProposal_ACCEPT,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var dataRootHash []byte
			var squareSize uint64
			if tc.expectedResult == abci.ResponseProcessProposal_ACCEPT {
				dataSquare, err := square.Construct(tc.txs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
				require.NoError(t, err)
				dataRootHash = calculateNewDataHash(t, tc.txs)
				squareSize = uint64(dataSquare.Size())
			}

			// ProcessProposal runs on a branched context that is discarded, so
			// state changes (like sequence increments) do not persist between sub-tests.
			resp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
				Height:       testApp.LastBlockHeight() + 1,
				Time:         time.Now(),
				Txs:          tc.txs,
				DataRootHash: dataRootHash,
				SquareSize:   squareSize,
			})
			require.NoError(t, err)
			require.Equal(t, tc.expectedResult, resp.Status)
		})
	}
}
