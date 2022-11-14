package blobtestutil

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

// GenerateManyRawWirePFB creates many raw WirePayForBlob transactions. Using
// negative numbers for count and size will randomize those values. count is
// capped at 5000 and size is capped at 3MB. Going over these caps will result
// in randomized values.
func GenerateManyRawWirePFB(t *testing.T, txConfig client.TxConfig, signer *types.KeyringSigner, count, size int) [][]byte {
	// hardcode a maximum of 5000 transactions so that we can use this for fuzzing
	if count > 5000 || count < 0 {
		count = tmrand.Intn(5000)
	}
	txs := make([][]byte, count)

	coin := sdk.Coin{
		Denom:  app.BondDenom,
		Amount: sdk.NewInt(10),
	}

	opts := []types.TxBuilderOption{
		types.SetFeeAmount(sdk.NewCoins(coin)),
		types.SetGasLimit(10000000),
	}

	for i := 0; i < count; i++ {
		if size < 0 || size > 3000000 {
			size = tmrand.Intn(1000000)
		}
		wpfbTx := generateRawWirePFBTx(
			t,
			txConfig,
			namespace.RandomMessageNamespace(),
			tmrand.Bytes(size),
			signer,
			opts...,
		)
		txs[i] = wpfbTx
	}
	return txs
}

func GenerateManyRawSendTxs(t *testing.T, txConfig client.TxConfig, signer *types.KeyringSigner, count int) [][]byte {
	txs := make([][]byte, count)
	for i := 0; i < count; i++ {
		txs[i] = generateRawSendTx(t, txConfig, signer, 100)
	}
	return txs
}

// this creates send transactions meant to help test encoding/prepare/process
// proposal, they are not meant to actually be executed by the state machine. If
// we want that, we have to update nonce, and send funds to someone other than
// the same account signing the transaction.
func generateRawSendTx(t *testing.T, txConfig client.TxConfig, signer *types.KeyringSigner, amount int64) (rawTx []byte) {
	feeCoin := sdk.Coin{
		Denom:  app.BondDenom,
		Amount: sdk.NewInt(1),
	}

	opts := []types.TxBuilderOption{
		types.SetFeeAmount(sdk.NewCoins(feeCoin)),
		types.SetGasLimit(1000000000),
	}

	amountCoin := sdk.Coin{
		Denom:  app.BondDenom,
		Amount: sdk.NewInt(amount),
	}

	addr, err := signer.GetSignerInfo().GetAddress()
	require.NoError(t, err)

	builder := signer.NewTxBuilder(opts...)

	msg := banktypes.NewMsgSend(addr, addr, sdk.NewCoins(amountCoin))

	tx, err := signer.BuildSignedTx(builder, msg)
	require.NoError(t, err)

	rawTx, err = txConfig.TxEncoder()(tx)
	require.NoError(t, err)

	return rawTx
}

// generateRawWirePFBTx creates a tx with a single MsgWirePayForBlob message using the provided namespace and message
func generateRawWirePFBTx(t *testing.T, txConfig client.TxConfig, ns, message []byte, signer *types.KeyringSigner, opts ...types.TxBuilderOption) (rawTx []byte) {
	// create a msg
	msg := generateSignedWirePayForBlob(t, ns, message, signer, opts)

	builder := signer.NewTxBuilder(opts...)
	tx, err := signer.BuildSignedTx(builder, msg)
	require.NoError(t, err)

	// encode the tx
	rawTx, err = txConfig.TxEncoder()(tx)
	require.NoError(t, err)

	return rawTx
}

func generateSignedWirePayForBlob(t *testing.T, ns, message []byte, signer *types.KeyringSigner, options []types.TxBuilderOption) *types.MsgWirePayForBlob {
	msg, err := types.NewWirePayForBlob(ns, message)
	if err != nil {
		t.Error(err)
	}

	err = msg.SignShareCommitment(signer, options...)
	if err != nil {
		t.Error(err)
	}

	return msg
}

const (
	TestAccountName = "test-account"
)
