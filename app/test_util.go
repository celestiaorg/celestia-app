package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

func GenerateValidBlockData(
	t *testing.T,
	txConfig client.TxConfig,
	signer *types.KeyringSigner,
	pfbCount,
	normalTxCount,
	size int,
) (coretypes.Data, error) {
	rawTxs := generateManyRawWirePFB(t, txConfig, signer, pfbCount, size)
	rawTxs = append(rawTxs, generateManyRawSendTxs(t, txConfig, signer, normalTxCount)...)
	parsedTxs := parseTxs(txConfig, rawTxs)

	squareSize, totalSharesUsed := estimateSquareSize(parsedTxs, core.EvidenceList{})

	if totalSharesUsed > int(squareSize*squareSize) {
		parsedTxs = prune(txConfig, parsedTxs, totalSharesUsed, int(squareSize))
	}

	processedTxs, blobs, err := malleateTxs(txConfig, squareSize, parsedTxs, core.EvidenceList{})
	require.NoError(t, err)

	blockData := core.Data{
		Txs:        processedTxs,
		Evidence:   core.EvidenceList{},
		Blobs:      blobs,
		SquareSize: squareSize,
	}

	return coretypes.DataFromProto(&blockData)
}

func generateManyRawWirePFB(t *testing.T, txConfig client.TxConfig, signer *types.KeyringSigner, count, size int) [][]byte {
	txs := make([][]byte, count)

	coin := sdk.Coin{
		Denom:  BondDenom,
		Amount: sdk.NewInt(10),
	}

	opts := []types.TxBuilderOption{
		types.SetFeeAmount(sdk.NewCoins(coin)),
		types.SetGasLimit(10000000),
	}

	for i := 0; i < count; i++ {
		wpfbTx := generateRawWirePFBTx(
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

func generateManyRawSendTxs(t *testing.T, txConfig client.TxConfig, signer *types.KeyringSigner, count int) [][]byte {
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
		Denom:  BondDenom,
		Amount: sdk.NewInt(1),
	}

	opts := []types.TxBuilderOption{
		types.SetFeeAmount(sdk.NewCoins(feeCoin)),
		types.SetGasLimit(1000000000),
	}

	amountCoin := sdk.Coin{
		Denom:  BondDenom,
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

// generateRawWirePFBTx creates a tx with a single MsgWirePayForBlob using
// the provided namespace, blob, and shareVersion
func generateRawWirePFBTx(t *testing.T, txConfig client.TxConfig, ns []byte, blob []byte, shareVersion uint8, signer *types.KeyringSigner, opts ...types.TxBuilderOption) (rawTx []byte) {
	// create a msg
	msg := generateSignedWirePayForBlob(t, ns, blob, shareVersion, signer, opts)

	builder := signer.NewTxBuilder(opts...)
	tx, err := signer.BuildSignedTx(builder, msg)
	require.NoError(t, err)

	// encode the tx
	rawTx, err = txConfig.TxEncoder()(tx)
	require.NoError(t, err)

	return rawTx
}

func generateSignedWirePayForBlob(t *testing.T, ns []byte, blob []byte, shareVersion uint8, signer *types.KeyringSigner, options []types.TxBuilderOption) *types.MsgWirePayForBlob {
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

const (
	TestAccountName = "test-account"
)
