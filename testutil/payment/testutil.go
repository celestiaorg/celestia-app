package paytestutil

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

// GenerateManyRawWirePFD creates many raw WirePayForData transactions. Using
// negative numbers for count and size will randomize those values. count is
// capped at 5000 and size is capped at 3MB. Going over these caps will result
// in randomized values.
func GenerateManyRawWirePFD(t *testing.T, txConfig client.TxConfig, signer *types.KeyringSigner, count, size int) [][]byte {
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
		wpfdTx := generateRawWirePFDTx(
			t,
			txConfig,
			randomValidNamespace(),
			tmrand.Bytes(size),
			signer,
			opts...,
		)
		txs[i] = wpfdTx
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

// generateRawWirePFDTx creates a tx with a single MsgWirePayForData message using the provided namespace and message
func generateRawWirePFDTx(t *testing.T, txConfig client.TxConfig, ns, message []byte, signer *types.KeyringSigner, opts ...types.TxBuilderOption) (rawTx []byte) {
	// create a msg
	msg := generateSignedWirePayForData(t, ns, message, signer, opts, types.AllSquareSizes(len(message))...)

	builder := signer.NewTxBuilder(opts...)
	tx, err := signer.BuildSignedTx(builder, msg)
	require.NoError(t, err)

	// encode the tx
	rawTx, err = txConfig.TxEncoder()(tx)
	require.NoError(t, err)

	return rawTx
}

func generateSignedWirePayForData(t *testing.T, ns, message []byte, signer *types.KeyringSigner, options []types.TxBuilderOption, ks ...uint64) *types.MsgWirePayForData {
	msg, err := types.NewWirePayForData(ns, message, ks...)
	if err != nil {
		t.Error(err)
	}

	err = msg.SignShareCommitments(signer, options...)
	if err != nil {
		t.Error(err)
	}

	return msg
}

const (
	TestAccountName = "test-account"
)

func randomValidNamespace() namespace.ID {
	for {
		s := tmrand.Bytes(8)
		if bytes.Compare(s, appconsts.MaxReservedNamespace) > 0 {
			return s
		}
	}
}
