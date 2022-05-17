package app_test

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/testutil"
	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestPrepareProposal(t *testing.T) {
	signer := testutil.GenerateKeyringSigner(t, testAccName)

	encCfg := encoding.MakeEncodingConfig(app.ModuleBasics.RegisterInterfaces)

	testApp := testutil.SetupTestAppWithGenesisValSet(t)

	type test struct {
		input            abci.RequestPrepareProposal
		expectedMessages []*core.Message
		expectedTxs      int
	}

	firstNS := []byte{2, 2, 2, 2, 2, 2, 2, 2}
	firstMessage := bytes.Repeat([]byte{4}, 512)
	firstRawTx := generateRawTx(t, encCfg.TxConfig, firstNS, firstMessage, signer, 2, 4, 8)

	secondNS := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	secondMessage := []byte{2}
	secondRawTx := generateRawTx(t, encCfg.TxConfig, secondNS, secondMessage, signer, 2, 4, 8)

	thirdNS := []byte{3, 3, 3, 3, 3, 3, 3, 3}
	thirdMessage := []byte{1}
	thirdRawTx := generateRawTx(t, encCfg.TxConfig, thirdNS, thirdMessage, signer, 2, 4, 8)

	tests := []test{
		{
			input: abci.RequestPrepareProposal{
				BlockData: &core.Data{
					Txs:                [][]byte{firstRawTx, secondRawTx, thirdRawTx},
					OriginalSquareSize: 4,
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
					Data:        append([]byte{1}, bytes.Repeat([]byte{0}, 255)...),
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

func generateRawTx(t *testing.T, txConfig client.TxConfig, ns, message []byte, signer *types.KeyringSigner, ks ...uint64) (rawTx []byte) {
	// create a msg
	msg := generateSignedWirePayForData(t, ns, message, signer, ks...)

	builder := signer.NewTxBuilder()

	coin := sdk.Coin{
		Denom:  app.BondDenom,
		Amount: sdk.NewInt(10),
	}

	builder.SetFeeAmount(sdk.NewCoins(coin))
	builder.SetGasLimit(1000000)
	builder.SetTimeoutHeight(99)

	tx, err := signer.BuildSignedTx(builder, msg)
	require.NoError(t, err)

	// encode the tx
	rawTx, err = txConfig.TxEncoder()(tx)
	require.NoError(t, err)

	return rawTx
}

func generateSignedWirePayForData(t *testing.T, ns, message []byte, signer *types.KeyringSigner, ks ...uint64) *types.MsgWirePayForData {
	msg, err := types.NewWirePayForData(ns, message, ks...)
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
