package app_test

import (
	"bytes"
	"fmt"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	tmrand "github.com/cometbft/cometbft/libs/rand"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/celestia-app/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
)

func TestProcessProposal(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig
	accounts := testfactory.GenerateAccounts(6)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	addr := testfactory.GetAddress(kr, accounts[0])
	signer, err := user.NewSigner(kr, nil, addr, enc, testutil.ChainID, infos[0].AccountNum, infos[0].Sequence)
	require.NoError(t, err)

	// create 4 single blob blobTxs that are signed with valid account numbers
	// and sequences
	blobTxs := blobfactory.ManyMultiBlobTx(
		t, enc, kr, testutil.ChainID, accounts[:4], infos[:4],
		blobfactory.NestedBlobs(
			t,
			appns.RandomBlobNamespaces(tmrand.NewRand(), 4),
			[][]int{{100}, {1000}, {420}, {300}},
		),
	)

	// create 3 MsgSend transactions that are signed with valid account numbers
	// and sequences
	sendTxs := testutil.SendTxsWithAccounts(
		t, testApp, enc, kr, 1000, accounts[0], accounts[len(accounts)-3:], testutil.ChainID,
	)

	// block with all blobs included
	validData := func() [][]byte {
		return blobTxs[:3]
	}

	mixedData := validData()
	mixedData = append(coretypes.Txs(sendTxs).ToSliceOfBytes(), mixedData...)

	// create an invalid block by adding an otherwise valid PFB, but an invalid
	// signature since there's no account
	badSigBlobTx := testutil.RandBlobTxsWithManualSequence(
		t, enc, kr, 1000, 1, false, testutil.ChainID, accounts[:1], 1, 1, true,
	)[0]

	blobTxWithInvalidNonce := testutil.RandBlobTxsWithManualSequence(
		t, enc, kr, 1000, 1, false, testutil.ChainID, accounts[:1], 1, 3, false,
	)[0]

	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))
	invalidNamespace, err := appns.New(appns.NamespaceVersionZero, bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))
	// expect an error because the input is invalid: it doesn't contain the namespace version zero prefix.
	assert.Error(t, err)
	data := bytes.Repeat([]byte{1}, 13)

	type test struct {
		name           string
		txs            [][]byte
		mutator        func([][]byte)
		expectedResult abci.ResponseProcessProposal_ProposalStatus
	}

	tests := []test{
		{
			name:           "valid untouched data",
			txs:            validData(),
			mutator:        func(txs [][]byte) {},
			expectedResult: abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name: "removed first blob tx",
			txs:  validData(),
			mutator: func(txs [][]byte) {
				txs = txs[1:]
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "added an extra blob tx",
			txs:  validData(),
			mutator: func(txs [][]byte) {
				hash := txs[len(txs)-1]
				txs = append(txs[:len(txs)-1], blobTxs[3], hash)
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "modified a blobTx",
			txs:  validData(),
			mutator: func(txs [][]byte) {
				blobTx, _ := coretypes.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0] = &tmproto.Blob{
					NamespaceId:      ns1.ID,
					Data:             data,
					NamespaceVersion: uint32(ns1.Version),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				}
				blobTxBytes, _ := blobTx.Marshal()
				txs[0] = blobTxBytes
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "invalid namespace TailPadding",
			txs:  validData(),
			mutator: func(txs [][]byte) {
				blobTx, _ := coretypes.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0] = &tmproto.Blob{
					NamespaceId:      appns.TailPaddingNamespace.ID,
					Data:             data,
					NamespaceVersion: uint32(appns.TailPaddingNamespace.Version),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				}
				blobTxBytes, _ := blobTx.Marshal()
				txs[0] = blobTxBytes
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "invalid namespace TxNamespace",
			txs:  validData(),
			mutator: func(txs [][]byte) {
				blobTx, _ := coretypes.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0] = &tmproto.Blob{
					NamespaceId:      appns.TxNamespace.ID,
					Data:             data,
					NamespaceVersion: uint32(appns.TxNamespace.Version),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				}
				blobTxBytes, _ := blobTx.Marshal()
				txs[0] = blobTxBytes
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "invalid namespace ParityShares",
			txs:  validData(),
			mutator: func(txs [][]byte) {
				blobTx, _ := coretypes.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0] = &tmproto.Blob{
					NamespaceId:      appns.ParitySharesNamespace.ID,
					Data:             data,
					NamespaceVersion: uint32(appns.ParitySharesNamespace.Version),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				}
				blobTxBytes, _ := blobTx.Marshal()
				txs[0] = blobTxBytes
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "invalid blob namespace",
			txs:  validData(),
			mutator: func(txs [][]byte) {
				blobTx, _ := coretypes.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0] = &tmproto.Blob{
					NamespaceId:      invalidNamespace.ID,
					Data:             data,
					ShareVersion:     uint32(appconsts.ShareVersionZero),
					NamespaceVersion: uint32(invalidNamespace.Version),
				}
				blobTxBytes, _ := blobTx.Marshal()
				txs[0] = blobTxBytes
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "pfb namespace version does not match blob",
			txs:  validData(),
			mutator: func(txs [][]byte) {
				blobTx, _ := coretypes.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0].NamespaceVersion = appns.NamespaceVersionMax
				blobTxBytes, _ := blobTx.Marshal()
				txs[0] = blobTxBytes
				// the last transaction is the hash which we need to update
				txs[len(txs)-1] = calculateNewDataHash(t, txs[:len(txs)-1])
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "invalid namespace in index wrapper tx",
			txs:  validData(),
			mutator: func(txs [][]byte) {
				index := 4
				tx, blob := blobfactory.IndexWrappedTxWithInvalidNamespace(t, tmrand.NewRand(), signer, uint32(index))
				blobTx, err := coretypes.MarshalBlobTx(tx, &blob)
				require.NoError(t, err)

				// Replace the data with new contents
				txs = [][]byte{blobTx}

				// Erasure code the data to update the data root so this doesn't doesn't fail on an incorrect data root.
				txs = append(txs, calculateNewDataHash(t, txs))
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "swap blobTxs",
			txs:  validData(),
			mutator: func(txs [][]byte) {
				// swapping the order will cause the data root to be different
				txs[0], txs[1], txs[2] = txs[1], txs[2], txs[0]
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "PFB without blobTx",
			txs:  validData(),
			mutator: func(txs [][]byte) {
				btx, _ := coretypes.UnmarshalBlobTx(blobTxs[3])
				txs[3] = btx.Tx
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "undecodable tx",
			txs:  validData(),
			mutator: func(txs [][]byte) {
				dataHash := txs[len(txs)-1]
				txs = append(txs[:len(txs)-1], tmrand.Bytes(300), dataHash)
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "incorrectly sorted; send tx after pfb",
			txs:  mixedData,
			mutator: func(txs [][]byte) {
				// swap txs at index 2 and 3 (essentially swapping a PFB with a normal tx)
				txs[3], txs[2] = txs[2], txs[3]
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "included pfb with bad signature",
			txs:  validData(),
			mutator: func(txs [][]byte) {
				txs = append(txs[:len(txs)-1], badSigBlobTx)
				txs = append(txs, calculateNewDataHash(t, txs))
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "included pfb with incorrect nonce",
			txs:  validData(),
			mutator: func(txs [][]byte) {
				txs = append(txs[:len(txs)-1], blobTxWithInvalidNonce)
				txs = append(txs, calculateNewDataHash(t, txs))
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "tampered sequence start",
			txs:  coretypes.Txs(sendTxs).ToSliceOfBytes(),
			mutator: func(txs [][]byte) {
				dataSquare, err := square.Construct(txs, appconsts.LatestVersion, appconsts.DefaultSquareSizeUpperBound)
				require.NoError(t, err)

				b := shares.NewEmptyBuilder().ImportRawShare(dataSquare[1].ToBytes())
				b.FlipSequenceStart()
				updatedShare, err := b.Build()
				require.NoError(t, err)
				dataSquare[1] = *updatedShare

				eds, err := da.ExtendShares(shares.ToBytes(dataSquare))
				require.NoError(t, err)

				dah, err := da.NewDataAvailabilityHeader(eds)
				require.NoError(t, err)
				// replace the hash of the prepare proposal response with the hash of a data
				// square with a tampered sequence start indicator
				txs[len(txs)-1] = dah.Hash()
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
				Txs: tt.txs,
			})
			require.Equal(t, len(tt.txs), len(resp.Txs))
			tt.mutator(resp.Txs)
			res := testApp.ProcessProposal(abci.RequestProcessProposal{
				Txs:    resp.Txs,
				Height: 1,
			})
			assert.Equal(t, tt.expectedResult, res.Status, fmt.Sprintf("expected %v, got %v", tt.expectedResult, res.Status))
		})
	}
}

func calculateNewDataHash(t *testing.T, txs [][]byte) []byte {
	dataSquare, err := square.Construct(txs, appconsts.LatestVersion, appconsts.DefaultSquareSizeUpperBound)
	require.NoError(t, err)
	eds, err := da.ExtendShares(shares.ToBytes(dataSquare))
	require.NoError(t, err)
	dah, err := da.NewDataAvailabilityHeader(eds)
	require.NoError(t, err)
	return dah.Hash()
}
