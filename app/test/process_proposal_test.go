package app_test

import (
	"bytes"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/testutil"
	"github.com/celestiaorg/celestia-app/testutil/blobfactory"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
)

func TestProcessProposal(t *testing.T) {
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
	sendTxs := testutil.SendTxsWithAccounts(
		t,
		testApp,
		encConf.TxConfig.TxEncoder(),
		kr,
		1000,
		accounts[0],
		accounts[len(accounts)-3:],
		"",
	)

	// block with all blobs included
	validData := func() *tmproto.Data {
		return &tmproto.Data{
			Txs: blobTxs,
		}
	}

	// create block data with a PFB that is not indexed and has no blob
	unindexedData := validData()
	blobtx := testutil.RandBlobTxsWithAccounts(
		t,
		testApp,
		encConf.TxConfig.TxEncoder(),
		kr,
		1000,
		2,
		false,
		"",
		accounts[:1],
	)[0]
	btx, _ := coretypes.UnmarshalBlobTx(blobtx)
	unindexedData.Txs = append(unindexedData.Txs, btx.Tx)

	// create block data with a tx that is random data, and therefore cannot be
	// decoded into an sdk.Tx
	undecodableData := validData()
	undecodableData.Txs = append(unindexedData.Txs, tmrand.Bytes(300))

	mixedData := validData()
	mixedData.Txs = append(mixedData.Txs, coretypes.Txs(sendTxs).ToSliceOfBytes()...)

	// create an invalid block by adding an otherwise valid PFB, but an invalid
	// signature since there's no account
	badSigPFBData := validData()
	badSigBlobTx := testutil.RandBlobTxsWithManualSequence(
		t,
		encConf.TxConfig.TxEncoder(),
		kr,
		1000,
		1,
		false,
		"",
		accounts[:1],
		420, 42,
	)[0]
	badSigPFBData.Txs = append(badSigPFBData.Txs, badSigBlobTx)

	type test struct {
		name           string
		input          *core.Data
		mutator        func(*core.Data)
		expectedResult abci.ResponseProcessProposal_Result
	}

	tests := []test{
		{
			name:           "valid untouched data",
			input:          validData(),
			mutator:        func(d *core.Data) {},
			expectedResult: abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name:  "removed first blob",
			input: validData(),
			mutator: func(d *core.Data) {
				d.Blobs = d.Blobs[1:]
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "added an extra blob",
			input: validData(),
			mutator: func(d *core.Data) {
				d.Blobs = append(
					d.Blobs,
					core.Blob{NamespaceId: []byte{1, 2, 3, 4, 5, 6, 7, 8}, Data: []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}},
				)
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "modified a blob",
			input: validData(),
			mutator: func(d *core.Data) {
				d.Blobs[0] = core.Blob{NamespaceId: []byte{1, 2, 3, 4, 5, 6, 7, 8}, Data: []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}}
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid namespace TailPadding",
			input: validData(),
			mutator: func(d *core.Data) {
				d.Blobs[0] = core.Blob{NamespaceId: appconsts.TailPaddingNamespaceID, Data: []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}}
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid namespace TxNamespace",
			input: validData(),
			mutator: func(d *core.Data) {
				d.Blobs[0] = core.Blob{NamespaceId: appconsts.TxNamespaceID, Data: []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}}
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "unsorted blobs",
			input: validData(),
			mutator: func(d *core.Data) {
				blob1, blob2, blob3 := d.Blobs[0], d.Blobs[1], d.Blobs[2]
				d.Blobs[0] = blob3
				d.Blobs[1] = blob1
				d.Blobs[2] = blob2
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:           "un-indexed PFB",
			input:          unindexedData,
			mutator:        func(d *core.Data) {},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:           "undecodable tx",
			input:          undecodableData,
			mutator:        func(d *core.Data) {},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "incorrectly sorted wrapped pfb's",
			input: mixedData,
			mutator: func(d *core.Data) {
				// swap txs at index 3 and 4 (essentially swapping a PFB with a normal tx)
				d.Txs[4], d.Txs[3] = d.Txs[3], d.Txs[4]
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			// while this test passes and the block gets rejected, it is getting
			// rejected because the data root is different. We need to refactor
			// prepare proposal to abstract functionality into a different
			// function or be able to skip the filtering checks. TODO: perform
			// the mentioned refactor and make it easier to create invalid
			// blocks for testing.
			name:  "included pfb with bad signature",
			input: validData(),
			mutator: func(d *core.Data) {
				btx, _ := coretypes.UnmarshalBlobTx(badSigBlobTx)
				d.Txs = append(d.Txs, btx.Tx)
				d.Blobs = append(d.Blobs, deref(btx.Blobs)...)
				sort.SliceStable(d.Blobs, func(i, j int) bool {
					return bytes.Compare(d.Blobs[i].NamespaceId, d.Blobs[j].NamespaceId) < 0
				})
				// todo: replace the data root with an updated hash
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "blob with parity namespace",
			input: validData(),
			mutator: func(d *tmproto.Data) {
				d.Blobs[len(d.Blobs)-1].NamespaceId = appconsts.ParitySharesNamespaceID
				// todo: replace the data root with an updated hash
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "tampered sequence start",
			input: &tmproto.Data{
				Txs: coretypes.Txs(sendTxs).ToSliceOfBytes(),
			},
			mutator: func(d *tmproto.Data) {
				bd, err := coretypes.DataFromProto(d)
				require.NoError(t, err)

				dataSquare, err := shares.Split(bd, true)
				require.NoError(t, err)

				b := shares.NewEmptyBuilder().ImportRawShare(dataSquare[1].ToBytes())
				b.FlipSequenceStart()
				updatedShare, err := b.Build()
				require.NoError(t, err)
				dataSquare[1] = *updatedShare

				eds, err := da.ExtendShares(d.SquareSize, shares.ToBytes(dataSquare))
				require.NoError(t, err)

				dah := da.NewDataAvailabilityHeader(eds)
				// replace the hash of the prepare proposal response with the hash of a data
				// square with a tampered sequence start indicator
				d.Hash = dah.Hash()
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
				BlockData: tt.input,
			})
			tt.mutator(resp.BlockData)
			res := testApp.ProcessProposal(abci.RequestProcessProposal{
				BlockData: resp.BlockData,
				Header: core.Header{
					DataHash: resp.BlockData.Hash,
				},
			})
			assert.Equal(t, tt.expectedResult, res.Result, tt.name)
		})
	}
}

func deref[T any](s []*T) []T {
	t := make([]T, len(s))
	for i, ss := range s {
		t[i] = *ss
	}
	return t
}
