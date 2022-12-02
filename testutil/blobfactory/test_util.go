package blobfactory

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	coretypes "github.com/tendermint/tendermint/types"
)

const (
	// bondDenom should match app.BondDenom. We copy it here so that we don't
	// have to import the application, causing an import cycle
	bondDenom = "utia"
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
		Denom:  bondDenom,
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
		wpfbTx := generateRawWirePFB(
			t,
			txConfig,
			namespace.RandomBlobNamespace(),
			tmrand.Bytes(size),
			appconsts.ShareVersionZero,
			signer,
			opts...,
		)
		txs[i] = wpfbTx
	}
	return txs
}

func GenerateRawWirePFB(t *testing.T, txConfig client.TxConfig, ns, blob []byte, signer *types.KeyringSigner) (rawTx []byte) {
	coin := sdk.Coin{
		Denom:  bondDenom,
		Amount: sdk.NewInt(10),
	}

	opts := []types.TxBuilderOption{
		types.SetFeeAmount(sdk.NewCoins(coin)),
		types.SetGasLimit(10000000),
	}

	return generateRawWirePFB(
		t,
		txConfig,
		ns,
		blob,
		appconsts.ShareVersionZero,
		signer,
		opts...,
	)
}

func GenerateSignedWirePayForBlob(t *testing.T, ns []byte, blob []byte, shareVersion uint8, signer *types.KeyringSigner, options []types.TxBuilderOption) *types.MsgWirePayForBlob {
	msg, err := types.NewWirePayForBlob(ns, blob, shareVersion)
	if err != nil {
		t.Error(err)
	}

	err = msg.SignShareCommitment(signer, options...)
	if err != nil {
		t.Error(err)
	}

	return msg
}

func GenerateManyRawSendTxs(txConfig client.TxConfig, count int) []coretypes.Tx {
	const acc = "signer"
	kr := testfactory.GenerateKeyring(acc)
	signer := blobtypes.NewKeyringSigner(kr, acc, "chainid")
	txs := make([]coretypes.Tx, count)
	for i := 0; i < count; i++ {
		txs[i] = generateRawSendTx(txConfig, signer, 100)
	}
	return txs
}

// generateRawSendTx creates send transactions meant to help test encoding/prepare/process
// proposal, they are not meant to actually be executed by the state machine. If
// we want that, we have to update nonce, and send funds to someone other than
// the same account signing the transaction.
func generateRawSendTx(txConfig client.TxConfig, signer *types.KeyringSigner, amount int64) (rawTx []byte) {
	feeCoin := sdk.Coin{
		Denom:  bondDenom,
		Amount: sdk.NewInt(1),
	}

	opts := []types.TxBuilderOption{
		types.SetFeeAmount(sdk.NewCoins(feeCoin)),
		types.SetGasLimit(1000000000),
	}

	amountCoin := sdk.Coin{
		Denom:  bondDenom,
		Amount: sdk.NewInt(amount),
	}

	addr, err := signer.GetSignerInfo().GetAddress()
	if err != nil {
		panic(err)
	}

	msg := banktypes.NewMsgSend(addr, addr, sdk.NewCoins(amountCoin))

	return genrateRawTx(txConfig, msg, signer, opts...)
}

// generateRawWirePFB creates a tx with a single MsgWirePayForBlob using
// the provided namespace, blob, and shareVersion
func generateRawWirePFB(t *testing.T, txConfig client.TxConfig, ns []byte, blob []byte, shareVersion uint8, signer *types.KeyringSigner, opts ...types.TxBuilderOption) (rawTx []byte) {
	msg := GenerateSignedWirePayForBlob(t, ns, blob, shareVersion, signer, opts)
	return genrateRawTx(txConfig, msg, signer, opts...)
}

func genrateRawTx(txConfig client.TxConfig, msg sdk.Msg, signer *types.KeyringSigner, opts ...types.TxBuilderOption) []byte {
	builder := signer.NewTxBuilder(opts...)
	tx, err := signer.BuildSignedTx(builder, msg)
	if err != nil {
		panic(err)
	}

	// encode the tx
	rawTx, err := txConfig.TxEncoder()(tx)
	if err != nil {
		panic(err)
	}

	return rawTx
}
