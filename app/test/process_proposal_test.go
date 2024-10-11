package app_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	"github.com/celestiaorg/celestia-app/v3/pkg/da"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/v2"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/go-square/v2/tx"
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
		blobfactory.DefaultTxOpts()...,
	)

	largeMemo := strings.Repeat("a", appconsts.MaxTxBytes(appconsts.LatestVersion))

	// create 2 single blobTxs that include a large memo making the transaction
	// larger than the configured max tx bytes
	largeBlobTxs := blobfactory.ManyMultiBlobTx(
		t, enc, kr, testutil.ChainID, accounts[3:], infos[3:],
		blobfactory.NestedBlobs(
			t,
			testfactory.RandomBlobNamespaces(tmrand.NewRand(), 4),
			[][]int{{100}, {1000}, {420}, {300}},
		),
		user.SetMemo(largeMemo))

	// create 1 large sendTx that includes a large memo making the
	// transaction over the configured max tx bytes limit
	largeSendTx := testutil.SendTxsWithAccounts(
		t, testApp, enc, kr, 1000, accounts[0], accounts[1:2], testutil.ChainID, user.SetMemo(largeMemo),
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

	ns1 := share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
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
				blobTx, _, err := tx.UnmarshalBlobTx(blobTxs[0])
				require.NoError(t, err)
				newBlob, err := share.NewBlob(ns1, data, share.ShareVersionZero, nil)
				require.NoError(t, err)
				blobTx.Blobs[0] = newBlob
				blobTxBytes, _ := tx.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
				d.Txs[0] = blobTxBytes
			},
			appVersion:     appconsts.LatestVersion,
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
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid namespace in index wrapper tx",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				index := 4
				transaction, b := blobfactory.IndexWrappedTxWithInvalidNamespace(t, tmrand.NewRand(), signer, uint32(index))
				blobTx, err := tx.MarshalBlobTx(transaction, b)
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
			appVersion:     appconsts.LatestVersion,
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
			appVersion:     appconsts.LatestVersion,
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
				msg, err := blobtypes.NewMsgPayForBlobs(addr.String(), appconsts.LatestVersion, blob)
				require.NoError(t, err)

				rawTx, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(100000), user.SetFee(100000))
				require.NoError(t, err)

				blobTxBytes, err := tx.MarshalBlobTx(rawTx, blob)
				require.NoError(t, err)
				d.Txs[0] = blobTxBytes
				d.Hash = calculateNewDataHash(t, d.Txs)
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
			appVersion:     v3.Version,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "blob txs larger than configured max tx bytes",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				d.Txs = append(d.Txs, largeBlobTxs...)
				d.Hash = calculateNewDataHash(t, d.Txs)
			},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "send tx larger than configured max tx bytes",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				d.Txs = append(coretypes.Txs(largeSendTx).ToSliceOfBytes(), d.Txs...)
				d.Hash = calculateNewDataHash(t, d.Txs)
			},
			appVersion:     appconsts.LatestVersion,
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
	eds, err := da.ExtendShares(share.ToBytes(dataSquare))
	require.NoError(t, err)
	dah, err := da.NewDataAvailabilityHeader(eds)
	require.NoError(t, err)
	return dah.Hash()
}
