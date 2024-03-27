package app_test

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
)

func TestProcessProposal(t *testing.T) {
	encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(6)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer := types.GenerateKeyringSigner(t, accounts[0])

	enc := encConf.TxConfig.TxEncoder()

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

	type test struct {
		name           string
		input          *tmproto.Data
		mutator        func(*tmproto.Data)
		expectedResult abci.ResponseProcessProposal_Result
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
				blobTx, _ := coretypes.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0] = &tmproto.Blob{
					NamespaceId:      ns1.ID,
					Data:             data,
					NamespaceVersion: uint32(ns1.Version),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				}
				blobTxBytes, _ := blobTx.Marshal()
				d.Txs[0] = blobTxBytes
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid namespace TailPadding",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				blobTx, _ := coretypes.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0] = &tmproto.Blob{
					NamespaceId:      appns.TailPaddingNamespace.ID,
					Data:             data,
					NamespaceVersion: uint32(appns.TailPaddingNamespace.Version),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				}
				blobTxBytes, _ := blobTx.Marshal()
				d.Txs[0] = blobTxBytes
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid namespace TxNamespace",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				blobTx, _ := coretypes.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0] = &tmproto.Blob{
					NamespaceId:      appns.TxNamespace.ID,
					Data:             data,
					NamespaceVersion: uint32(appns.TxNamespace.Version),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				}
				blobTxBytes, _ := blobTx.Marshal()
				d.Txs[0] = blobTxBytes
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid namespace ParityShares",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				blobTx, _ := coretypes.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0] = &tmproto.Blob{
					NamespaceId:      appns.ParitySharesNamespace.ID,
					Data:             data,
					NamespaceVersion: uint32(appns.ParitySharesNamespace.Version),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				}
				blobTxBytes, _ := blobTx.Marshal()
				d.Txs[0] = blobTxBytes
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid blob namespace",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				blobTx, _ := coretypes.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0] = &tmproto.Blob{
					NamespaceId:      invalidNamespace.ID,
					Data:             data,
					ShareVersion:     uint32(appconsts.ShareVersionZero),
					NamespaceVersion: uint32(invalidNamespace.Version),
				}
				blobTxBytes, _ := blobTx.Marshal()
				d.Txs[0] = blobTxBytes
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "pfb namespace version does not match blob",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				blobTx, _ := coretypes.UnmarshalBlobTx(blobTxs[0])
				blobTx.Blobs[0].NamespaceVersion = appns.NamespaceVersionMax
				blobTxBytes, _ := blobTx.Marshal()
				d.Txs[0] = blobTxBytes
				d.Hash = calculateNewDataHash(t, d.Txs)
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid namespace in index wrapper tx",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
				index := 4
				tx, blob := blobfactory.IndexWrappedTxWithInvalidNamespace(t, encCfg.TxConfig.TxEncoder(), tmrand.NewRand(), signer, 0, 0, uint32(index))
				blobTx, err := coretypes.MarshalBlobTx(tx, &blob)
				require.NoError(t, err)

				// Replace the data with new contents
				d.Txs = [][]byte{blobTx}

				// Erasure code the data to update the data root so this doesn't doesn't fail on an incorrect data root.
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
			name:  "undecodable tx",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				d.Txs = append(d.Txs, tmrand.Bytes(300))
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
				dataSquare, err := square.Construct(d.Txs, appconsts.LatestVersion, appconsts.DefaultSquareSizeUpperBound)
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
				},
			})
			assert.Equal(t, tt.expectedResult, res.Result, fmt.Sprintf("expected %v, got %v", tt.expectedResult, res.Result))
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
