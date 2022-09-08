package app_test

import (
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/celestiaorg/nmt/namespace"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/pkg/consts"
	"github.com/tendermint/tendermint/pkg/da"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	shares "github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/testutil"
	paytestutil "github.com/celestiaorg/celestia-app/testutil/payment"
	"github.com/celestiaorg/celestia-app/x/payment/types"
)

func TestMessageInclusionCheck(t *testing.T) {
	signer := testutil.GenerateKeyringSigner(t, testAccName)
	testApp := testutil.SetupTestAppWithGenesisValSet(t)
	encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	// block with all messages included
	validData := func() *core.Data {
		return &core.Data{
			Txs: paytestutil.GenerateManyRawWirePFD(t, encConf.TxConfig, signer, 4, 1000),
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

func TestProcessMessagesWithReservedNamespaces(t *testing.T) {
	testApp := testutil.SetupTestAppWithGenesisValSet(t)
	encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	signer := testutil.GenerateKeyringSigner(t, testAccName)

	type test struct {
		name           string
		namespace      namespace.ID
		expectedResult abci.ResponseProcessProposal_Result
	}

	tests := []test{
		{"transaction namespace id for message", consts.TxNamespaceID, abci.ResponseProcessProposal_REJECT},
		{"evidence namespace id for message", consts.EvidenceNamespaceID, abci.ResponseProcessProposal_REJECT},
		{"tail padding namespace id for message", consts.TailPaddingNamespaceID, abci.ResponseProcessProposal_REJECT},
		{"namespace id 200 for message", namespace.ID{0, 0, 0, 0, 0, 0, 0, 200}, abci.ResponseProcessProposal_REJECT},
		{"correct namespace id for message", namespace.ID{3, 3, 2, 2, 2, 1, 1, 1}, abci.ResponseProcessProposal_ACCEPT},
	}

	for _, tt := range tests {
		pfd, msg := genRandMsgPayForDataForNamespace(t, signer, 8, tt.namespace)
		input := abci.RequestProcessProposal{
			BlockData: &core.Data{
				Txs: [][]byte{
					buildTx(t, signer, encConf.TxConfig, pfd),
				},
				Messages: core.Messages{
					MessagesList: []*core.Message{
						{
							NamespaceId: pfd.GetMessageNamespaceId(),
							Data:        msg,
						},
					},
				},
				OriginalSquareSize: 8,
			},
		}
		data, err := coretypes.DataFromProto(input.BlockData)
		require.NoError(t, err)

		shares, err := shares.Split(data)
		require.NoError(t, err)

		require.NoError(t, err)
		eds, err := da.ExtendShares(input.BlockData.OriginalSquareSize, shares)
		require.NoError(t, err)
		dah := da.NewDataAvailabilityHeader(eds)
		input.Header.DataHash = dah.Hash()
		res := testApp.ProcessProposal(input)
		assert.Equal(t, tt.expectedResult, res.Result)
	}
}

func TestProcessMessageWithUnsortedMessages(t *testing.T) {
	testApp := testutil.SetupTestAppWithGenesisValSet(t)
	encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	signer := testutil.GenerateKeyringSigner(t, testAccName)

	namespaceOne := namespace.ID{1, 1, 1, 1, 1, 1, 1, 1}
	namespaceTwo := namespace.ID{2, 2, 2, 2, 2, 2, 2, 2}

	pfdOne, msgOne := genRandMsgPayForDataForNamespace(t, signer, 8, namespaceOne)
	pfdTwo, msgTwo := genRandMsgPayForDataForNamespace(t, signer, 8, namespaceTwo)

	cMsgOne := &core.Message{NamespaceId: pfdOne.GetMessageNamespaceId(), Data: msgOne}
	cMsgTwo := &core.Message{NamespaceId: pfdTwo.GetMessageNamespaceId(), Data: msgTwo}

	input := abci.RequestProcessProposal{
		BlockData: &core.Data{
			Txs: [][]byte{
				buildTx(t, signer, encConf.TxConfig, pfdOne),
				buildTx(t, signer, encConf.TxConfig, pfdTwo),
			},
			Messages: core.Messages{
				MessagesList: []*core.Message{
					cMsgOne,
					cMsgTwo,
				},
			},
			OriginalSquareSize: 8,
		},
	}
	data, err := coretypes.DataFromProto(input.BlockData)
	require.NoError(t, err)

	shares, err := shares.Split(data)
	require.NoError(t, err)

	require.NoError(t, err)
	eds, err := da.ExtendShares(input.BlockData.OriginalSquareSize, shares)

	require.NoError(t, err)
	dah := da.NewDataAvailabilityHeader(eds)
	input.Header.DataHash = dah.Hash()

	// swap the messages
	input.BlockData.Messages.MessagesList[0] = cMsgTwo
	input.BlockData.Messages.MessagesList[1] = cMsgOne

	got := testApp.ProcessProposal(input)

	assert.Equal(t, got.Result, abci.ResponseProcessProposal_REJECT)
}

func TestProcessMessageWithParityShareNamespaces(t *testing.T) {
	testApp := testutil.SetupTestAppWithGenesisValSet(t)
	encConf := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	signer := testutil.GenerateKeyringSigner(t, testAccName)

	pfd, msg := genRandMsgPayForDataForNamespace(t, signer, 8, consts.ParitySharesNamespaceID)
	input := abci.RequestProcessProposal{
		BlockData: &core.Data{
			Txs: [][]byte{
				buildTx(t, signer, encConf.TxConfig, pfd),
			},
			Messages: core.Messages{
				MessagesList: []*core.Message{
					{
						NamespaceId: pfd.GetMessageNamespaceId(),
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

func genRandMsgPayForDataForNamespace(t *testing.T, signer *types.KeyringSigner, squareSize uint64, ns namespace.ID) (*types.MsgPayForData, []byte) {
	message := make([]byte, randomInt(20))
	_, err := rand.Read(message)
	require.NoError(t, err)

	commit, err := types.CreateCommitment(squareSize, ns, message)
	require.NoError(t, err)

	pfd := types.MsgPayForData{
		MessageShareCommitment: commit,
		MessageNamespaceId:     ns,
	}

	return &pfd, message
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
