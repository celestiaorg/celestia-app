package app_test

import (
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	core "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil"
	paytestutil "github.com/celestiaorg/celestia-app/testutil/blob"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/celestiaorg/nmt/namespace"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestMessageInclusionCheck(t *testing.T) {
	signer := testutil.GenerateKeyringSigner(t, testAccName)
	testApp := testutil.SetupTestAppWithGenesisValSet(t)
	encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	// block with all messages included
	validData := func() *core.Data {
		return &core.Data{
			Txs: paytestutil.GenerateManyRawWirePFB(t, encConf.TxConfig, signer, 4, 1000),
		}
	}

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
			name:  "removed first message",
			input: validData(),
			mutator: func(d *core.Data) {
				d.Messages.MessagesList = d.Messages.MessagesList[1:]
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "added an extra message",
			input: validData(),
			mutator: func(d *core.Data) {
				d.Messages.MessagesList = append(
					d.Messages.MessagesList,
					&core.Message{NamespaceId: []byte{1, 2, 3, 4, 5, 6, 7, 8}, Data: []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}},
				)
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "modified a message",
			input: validData(),
			mutator: func(d *core.Data) {
				d.Messages.MessagesList[0] = &core.Message{NamespaceId: []byte{1, 2, 3, 4, 5, 6, 7, 8}, Data: []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}}
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid namespace TailPadding",
			input: validData(),
			mutator: func(d *core.Data) {
				d.Messages.MessagesList[0] = &core.Message{NamespaceId: appconsts.TailPaddingNamespaceID, Data: []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}}
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "invalid namespace TxNamespace",
			input: validData(),
			mutator: func(d *core.Data) {
				d.Messages.MessagesList[0] = &core.Message{NamespaceId: appconsts.TxNamespaceID, Data: []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}}
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:  "unsorted messages",
			input: validData(),
			mutator: func(d *core.Data) {
				msg1, msg2, msg3 := d.Messages.MessagesList[0], d.Messages.MessagesList[1], d.Messages.MessagesList[2]
				d.Messages.MessagesList[0] = msg3
				d.Messages.MessagesList[1] = msg1
				d.Messages.MessagesList[2] = msg2
			},
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
	}

	for _, tt := range tests {
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
	}
}

// TODO: redo this tests, which is more difficult to do now that it requires the
// data to be processed by PrepareProposal func
// TestProcessMessagesWithReservedNamespaces(t *testing.T) {
//  testApp := testutil.SetupTestAppWithGenesisValSet(t)
//  encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)

// 	signer := testutil.GenerateKeyringSigner(t, testAccName)

// 	type test struct {
// 		name           string
// 		namespace      namespace.ID
// 		expectedResult abci.ResponseProcessProposal_Result
// 	}

// 	tests := []test{
// 		{"transaction namespace id for message", appconsts.TxNamespaceID, abci.ResponseProcessProposal_REJECT},
// 		{"evidence namespace id for message", appconsts.EvidenceNamespaceID, abci.ResponseProcessProposal_REJECT},
// 		{"tail padding namespace id for message", appconsts.TailPaddingNamespaceID, abci.ResponseProcessProposal_REJECT},
// 		{"namespace id 200 for message", namespace.ID{0, 0, 0, 0, 0, 0, 0, 200}, abci.ResponseProcessProposal_REJECT},
// 		{"correct namespace id for message", namespace.ID{3, 3, 2, 2, 2, 1, 1, 1}, abci.ResponseProcessProposal_ACCEPT},
// 	}

// 	for _, tt := range tests {
// 		pfb, msg := genRandMsgPayForBlobForNamespace(t, signer, 8, tt.namespace)
// 		input := abci.RequestProcessProposal{
// 			BlockData: &core.Data{
// 				Txs: [][]byte{
// 					buildTx(t, signer, encConf.TxConfig, pfb),
// 				},
// 				Messages: core.Messages{
// 					MessagesList: []*core.Message{
// 						{
// 							NamespaceId: pfb.GetNamespaceId(),
// 							Data:        msg,
// 						},
// 					},
// 				},
// 				OriginalSquareSize: 8,
// 			},
// 		}
// 		data, err := coretypes.DataFromProto(input.BlockData)
// 		require.NoError(t, err)

// 		shares, err := shares.Split(data)
// 		require.NoError(t, err)

// 		require.NoError(t, err)
// 		eds, err := da.ExtendShares(input.BlockData.OriginalSquareSize, shares)
// 		require.NoError(t, err)
// 		dah := da.NewDataAvailabilityHeader(eds)
// 		input.Header.DataHash = dah.Hash()
// 		res := testApp.ProcessProposal(input)
// 		assert.Equal(t, tt.expectedResult, res.Result)
// 	}
// }

func TestProcessMessageWithParityShareNamespaces(t *testing.T) {
	testApp := testutil.SetupTestAppWithGenesisValSet(t)
	encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	signer := testutil.GenerateKeyringSigner(t, testAccName)

	pfb, msg := genRandMsgPayForBlobForNamespace(t, signer, 8, appconsts.ParitySharesNamespaceID)
	input := abci.RequestProcessProposal{
		BlockData: &core.Data{
			Txs: [][]byte{
				buildTx(t, signer, encConf.TxConfig, pfb),
			},
			Messages: core.Messages{
				MessagesList: []*core.Message{
					{
						NamespaceId: pfb.GetNamespaceId(),
						Data:        msg,
					},
				},
			},
			OriginalSquareSize: 8,
		},
	}
	res := testApp.ProcessProposal(input)
	assert.Equal(t, abci.ResponseProcessProposal_REJECT, res.Result)
}

func genRandMsgPayForBlobForNamespace(t *testing.T, signer *types.KeyringSigner, squareSize uint64, ns namespace.ID) (*types.MsgPayForBlob, []byte) {
	message := make([]byte, randomInt(20))
	_, err := rand.Read(message)
	require.NoError(t, err)

	commit, err := types.CreateCommitment(squareSize, ns, message)
	require.NoError(t, err)

	pfb := types.MsgPayForBlob{
		ShareCommitment: commit,
		NamespaceId:     ns,
	}

	return &pfb, message
}

func buildTx(t *testing.T, signer *types.KeyringSigner, txCfg client.TxConfig, msg sdk.Msg) []byte {
	tx, err := signer.BuildSignedTx(signer.NewTxBuilder(), msg)
	require.NoError(t, err)

	rawTx, err := txCfg.TxEncoder()(tx)
	require.NoError(t, err)

	return rawTx
}

func randomInt(max int64) int64 {
	i, _ := rand.Int(rand.Reader, big.NewInt(max))
	return i.Int64()
}
