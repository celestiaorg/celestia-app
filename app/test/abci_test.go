package app_test

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/testutil"
	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/spm/cosmoscmd"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/pkg/consts"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestPrepareProposal(t *testing.T) {
	signer := testutil.GenerateKeyringSigner(t, testAccName)
	info := signer.GetSignerInfo()

	encCfg := cosmoscmd.MakeEncodingConfig(app.ModuleBasics)

	testApp := testutil.SetupTestApp(t, info.GetAddress())

	type test struct {
		input            abci.RequestPrepareProposal
		expectedMessages []*core.Message
		expectedTxs      int
	}

	firstNS := []byte{2, 2, 2, 2, 2, 2, 2, 2}
	firstMessage := bytes.Repeat([]byte{2}, 512)
	firstRawTx := generateRawTx(t, encCfg.TxConfig, firstNS, firstMessage, signer)

	secondNS := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	secondMessage := []byte{2}
	secondRawTx := generateRawTx(t, encCfg.TxConfig, secondNS, secondMessage, signer)

	thirdNS := []byte{3, 3, 3, 3, 3, 3, 3, 3}
	thirdMessage := []byte{}
	thirdRawTx := generateRawTx(t, encCfg.TxConfig, thirdNS, thirdMessage, signer)

	tests := []test{
		{
			input: abci.RequestPrepareProposal{
				BlockData: &core.Data{
					Txs: [][]byte{firstRawTx, secondRawTx, thirdRawTx},
				},
			},
			expectedMessages: []*core.Message{
				{
					NamespaceId: secondNS,                                           // the second message should be first
					Data:        append([]byte{2}, bytes.Repeat([]byte{0}, 255)...), // check that the message is padded
				},
				{
					NamespaceId: firstNS,
					Data:        firstMessage,
				},
				{
					NamespaceId: thirdNS,
					Data:        nil,
				},
			},
			expectedTxs: 3,
		},
	}

	for _, tt := range tests {
		res := testApp.PrepareProposal(tt.input)
		assert.Equal(t, tt.expectedMessages, res.BlockData.Messages.MessagesList)
		assert.Equal(t, tt.expectedTxs, len(res.BlockData.Txs))
	}
}

func generateRawTx(t *testing.T, txConfig client.TxConfig, ns, message []byte, signer *types.KeyringSigner) (rawTx []byte) {
	// create a msg
	msg := generateSignedWirePayForMessage(t, consts.MaxSquareSize, ns, message, signer)

	builder := signer.NewTxBuilder()

	coin := sdk.Coin{
		Denom:  "token",
		Amount: sdk.NewInt(1000),
	}

	builder.SetFeeAmount(sdk.NewCoins(coin))
	builder.SetGasLimit(10000)
	builder.SetTimeoutHeight(99)

	tx, err := signer.BuildSignedTx(builder, msg)
	require.NoError(t, err)

	// encode the tx
	rawTx, err = txConfig.TxEncoder()(tx)
	require.NoError(t, err)

	return rawTx
}

func generateSignedWirePayForMessage(t *testing.T, k uint64, ns, message []byte, signer *types.KeyringSigner) *types.MsgWirePayForMessage {
	msg, err := types.NewWirePayForMessage(ns, message, k)
	if err != nil {
		t.Error(err)
	}

	err = msg.SignShareCommitments(signer)
	if err != nil {
		t.Error(err)
	}

	return msg
}

const (
	testAccName = "test-account"
)
