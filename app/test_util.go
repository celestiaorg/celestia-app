package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
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

	processedTxs, messages, err := malleateTxs(txConfig, squareSize, parsedTxs, core.EvidenceList{})
	require.NoError(t, err)

	blockData := core.Data{
		Txs:                processedTxs,
		Evidence:           core.EvidenceList{},
		Messages:           core.Messages{MessagesList: messages},
		OriginalSquareSize: squareSize,
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
			namespace.RandomMessageNamespace(),
			tmrand.Bytes(size),
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

// generateRawWirePFB creates a tx with a single MsgWirePayForBlob message using the provided namespace and message
func generateRawWirePFBTx(t *testing.T, txConfig client.TxConfig, ns, message []byte, signer *types.KeyringSigner, opts ...types.TxBuilderOption) (rawTx []byte) {
	// create a msg
	msg := generateSignedWirePayForBlob(t, ns, message, signer, opts, types.AllSquareSizes(len(message))...)

	builder := signer.NewTxBuilder(opts...)
	tx, err := signer.BuildSignedTx(builder, msg)
	require.NoError(t, err)

	// encode the tx
	rawTx, err = txConfig.TxEncoder()(tx)
	require.NoError(t, err)

	return rawTx
}

func generateSignedWirePayForBlob(t *testing.T, ns, message []byte, signer *types.KeyringSigner, options []types.TxBuilderOption, ks ...uint64) *types.MsgWirePayForBlob {
	msg, err := types.NewWirePayForBlob(ns, message, ks...)
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

func generateKeyring(t *testing.T, cdc codec.Codec, accts ...string) keyring.Keyring {
	t.Helper()
	kb := keyring.NewInMemory(cdc)

	for _, acc := range accts {
		_, _, err := kb.NewMnemonic(acc, keyring.English, "", "", hd.Secp256k1)
		if err != nil {
			t.Error(err)
		}
	}

	_, err := kb.NewAccount(testAccName, testMnemo, "1234", "", hd.Secp256k1)
	if err != nil {
		panic(err)
	}

	return kb
}

// generateKeyringSigner creates a types.KeyringSigner with keys generated for
// the provided accounts
func generateKeyringSigner(t *testing.T, acct string) *types.KeyringSigner {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	kr := generateKeyring(t, encCfg.Codec, acct)
	return types.NewKeyringSigner(kr, acct, testChainID)
}

const (
	// nolint:lll
	testMnemo   = `ramp soldier connect gadget domain mutual staff unusual first midnight iron good deputy wage vehicle mutual spike unlock rocket delay hundred script tumble choose`
	testAccName = "test-account"
	testChainID = "test-chain-1"
)
