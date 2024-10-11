package app_test

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v2/pkg/da"
	"github.com/celestiaorg/celestia-app/v2/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testfactory"
	"github.com/celestiaorg/go-square/blob"
	appns "github.com/celestiaorg/go-square/namespace"
	"github.com/celestiaorg/go-square/shares"
	"github.com/celestiaorg/go-square/square"
)

func TestProcessProposal(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig
	accounts := testfactory.GenerateAccounts(6)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer, err := user.NewSigner(kr, enc, testutil.ChainID, appconsts.LatestVersion, user.NewAccount(accounts[0], infos[0].AccountNum, infos[0].Sequence))
	require.NoError(t, err)

	// create 4 single blob blobTxs that are signed with valid account numbers
	// and sequences
	blobTxs := blobfactory.ManyMultiBlobTx(
		t, enc, kr, testutil.ChainID, accounts[:4], infos[:4],
		blobfactory.NestedBlobs(
			t,
			testfactory.RandomBlobNamespaces(tmrand.NewRand(), 4),
			[][]int{{100}, {1000}, {420}, {300}},
		),
	)

	// create 3 MsgSend transactions that are signed with valid account numbers
	// and sequences
	sendTxs := testutil.SendTxsWithAccounts(
		t, testApp, enc, kr, 1000, accounts[0], accounts[len(accounts)-3:], testutil.ChainID,
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

	tooManyShareBtx := blobfactory.ManyMultiBlobTx(
		t,
		enc,
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

	type test struct {
		name           string
		input          *tmproto.Data
		mutator        func(*tmproto.Data)
		appVersion     uint64
		expectedResult abci.ResponseProcessProposal_Result
	}

	tests := []test{
		{
			name:           "valid untouched data",
			input:          validData(),
			mutator:        func(_ *tmproto.Data) {},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name:  "removed first blob tx",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				d.Txs = d.Txs[1:]
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "added an extra blob tx",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				d.Txs = append(d.Txs, blobTxs[3])
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "modified a blobTx",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				blobTx, _ := blob.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0] = &blob.Blob{
					NamespaceId:      ns1.ID,
					Data:             data,
					NamespaceVersion: uint32(ns1.Version),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				}
				blobTxBytes, _ := blob.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
				d.Txs[0] = blobTxBytes
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid namespace TailPadding",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				blobTx, _ := blob.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0] = &blob.Blob{
					NamespaceId:      appns.TailPaddingNamespace.ID,
					Data:             data,
					NamespaceVersion: uint32(appns.TailPaddingNamespace.Version),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				}
				blobTxBytes, _ := blob.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
				d.Txs[0] = blobTxBytes
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid namespace TxNamespace",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				blobTx, _ := blob.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0] = &blob.Blob{
					NamespaceId:      appns.TxNamespace.ID,
					Data:             data,
					NamespaceVersion: uint32(appns.TxNamespace.Version),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				}
				blobTxBytes, _ := blob.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
				d.Txs[0] = blobTxBytes
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid namespace ParityShares",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				blobTx, _ := blob.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0] = &blob.Blob{
					NamespaceId:      appns.ParitySharesNamespace.ID,
					Data:             data,
					NamespaceVersion: uint32(appns.ParitySharesNamespace.Version),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				}
				blobTxBytes, _ := blob.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
				d.Txs[0] = blobTxBytes
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid blob namespace",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				blobTx, _ := blob.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0] = &blob.Blob{
					NamespaceId:      invalidNamespace.ID,
					Data:             data,
					ShareVersion:     uint32(appconsts.ShareVersionZero),
					NamespaceVersion: uint32(invalidNamespace.Version),
				}
				blobTxBytes, _ := blob.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
				d.Txs[0] = blobTxBytes
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "pfb namespace version does not match blob",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				blobTx, _ := blob.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0].NamespaceVersion = appns.NamespaceVersionMax
				blobTxBytes, _ := blob.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
				d.Txs[0] = blobTxBytes
				d.Hash = calculateNewDataHash(t, d.Txs)
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid namespace in index wrapper tx",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				index := 4
				tx, b := blobfactory.IndexWrappedTxWithInvalidNamespace(t, tmrand.NewRand(), signer, uint32(index))
				blobTx, err := blob.MarshalBlobTx(tx, b)
				require.NoError(t, err)

				// Replace the data with new contents
				d.Txs = [][]byte{blobTx}

				// Erasure code the data to update the data root so this doesn't doesn't fail on an incorrect data root.
				d.Hash = calculateNewDataHash(t, d.Txs)
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "swap blobTxs",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				// swapping the order will cause the data root to be different
				d.Txs[0], d.Txs[1], d.Txs[2] = d.Txs[1], d.Txs[2], d.Txs[0]
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "PFB without blobTx",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				btx, _ := coretypes.UnmarshalBlobTx(blobTxs[3])
				d.Txs = append(d.Txs, btx.Tx)
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "undecodable tx with app version 1",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				d.Txs = append([][]byte{tmrand.Bytes(300)}, d.Txs...)
				// Update the data hash so that the test doesn't fail due to an incorrect data root.
				d.Hash = calculateNewDataHash(t, d.Txs)
			},
			appVersion:     v1.Version,
			expectedResult: abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name:  "undecodable tx with app version 2",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				d.Txs = append([][]byte{tmrand.Bytes(300)}, d.Txs...)
				// Update the data hash so that the test doesn't fail due to an incorrect data root.
				d.Hash = calculateNewDataHash(t, d.Txs)
			},
			appVersion:     v2.Version,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "incorrectly sorted; send tx after pfb",
			input: mixedData,
			mutator: func(d *tmproto.Data) {
				// swap txs at index 2 and 3 (essentially swapping a PFB with a normal tx)
				d.Txs[3], d.Txs[2] = d.Txs[2], d.Txs[3]
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "included pfb with bad signature",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				d.Txs = append(d.Txs, badSigBlobTx)
				d.Hash = calculateNewDataHash(t, d.Txs)
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "included pfb with incorrect nonce",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				d.Txs = append(d.Txs, blobTxWithInvalidNonce)
				d.Hash = calculateNewDataHash(t, d.Txs)
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "tampered sequence start",
			input: &tmproto.Data{
				Txs: coretypes.Txs(sendTxs).ToSliceOfBytes(),
			},
			mutator: func(d *tmproto.Data) {
				dataSquare, err := square.Construct(d.Txs, appconsts.DefaultSquareSizeUpperBound, appconsts.DefaultSubtreeRootThreshold)
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
				d.Hash = dah.Hash()
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "blob tx that takes up too many shares",
			input: &tmproto.Data{
				Txs: [][]byte{},
			},
			mutator: func(d *tmproto.Data) {
				// this tx will get filtered out by prepare proposal before this
				// so we add it here
				d.Txs = append(d.Txs, tooManyShareBtx)
			},
			appVersion:     v2.Version,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			height := testApp.LastBlockHeight() + 1
			blockTime := time.Now()

			resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
				BlockData: tt.input,
				ChainId:   testutil.ChainID,
				Height:    height,
				Time:      blockTime,
			})
			require.Equal(t, len(tt.input.Txs), len(resp.BlockData.Txs))
			tt.mutator(resp.BlockData)
			res := testApp.ProcessProposal(abci.RequestProcessProposal{
				BlockData: resp.BlockData,
				Header: tmproto.Header{
					Height:   1,
					DataHash: resp.BlockData.Hash,
					ChainID:  testutil.ChainID,
					Version: version.Consensus{
						App: tt.appVersion,
					},
				},
			})
			assert.Equal(t, tt.expectedResult, res.Result, fmt.Sprintf("expected %v, got %v", tt.expectedResult, res.Result))
		})
	}
}

func calculateNewDataHash(t *testing.T, txs [][]byte) []byte {
	dataSquare, err := square.Construct(txs, appconsts.DefaultSquareSizeUpperBound, appconsts.DefaultSubtreeRootThreshold)
	require.NoError(t, err)
	eds, err := da.ExtendShares(shares.ToBytes(dataSquare))
	require.NoError(t, err)
	dah, err := da.NewDataAvailabilityHeader(eds)
	require.NoError(t, err)
	return dah.Hash()
}
