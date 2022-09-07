package app

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/pkg/consts"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

func Test_estimateSquareSize(t *testing.T) {
	type test struct {
		name                  string
		normalTxs             int
		wPFDCount, messgeSize int
		expectedSize          uint64
	}
	tests := []test{
		{"empty block minimum square size", 0, 0, 0, 2},
		{"full block with only txs", 10000, 0, 0, consts.MaxSquareSize},
		{"random small block square size 4", 0, 1, 400, 4},
		{"random small block square size 8", 0, 1, 2000, 8},
		{"random small block w/ 10 nomaml txs square size 4", 10, 1, 2000, 8},
		{"random small block square size 16", 0, 4, 2000, 16},
		{"random medium block square size 32", 0, 50, 2000, 32},
		{"full block max square size", 0, 8000, 100, consts.MaxSquareSize},
		{"overly full block", 0, 80, 100000, consts.MaxSquareSize},
		{"one over the perfect estimation edge case", 10, 1, 300, 8},
	}
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	signer := generateKeyringSigner(t, "estimate-key")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txs := generateManyRawWirePFD(t, encConf.TxConfig, signer, tt.wPFDCount, tt.messgeSize)
			txs = append(txs, generateManyRawSendTxs(t, encConf.TxConfig, signer, tt.normalTxs)...)
			parsedTxs := parseTxs(encConf.TxConfig, txs)
			squareSize, totalSharesUsed := estimateSquareSize(parsedTxs, core.EvidenceList{})
			assert.Equal(t, tt.expectedSize, squareSize)

			if totalSharesUsed > int(squareSize*squareSize) {
				parsedTxs = prune(encConf.TxConfig, parsedTxs, totalSharesUsed, int(squareSize))
			}

			processedTxs, messages, err := malleateTxs(encConf.TxConfig, squareSize, parsedTxs, core.EvidenceList{})
			require.NoError(t, err)

			blockData := coretypes.Data{
				Txs:                shares.TxsFromBytes(processedTxs),
				Evidence:           coretypes.EvidenceData{},
				Messages:           coretypes.Messages{MessagesList: shares.MessagesFromProto(messages)},
				OriginalSquareSize: squareSize,
			}

			rawShares, err := shares.Split(blockData)
			require.NoError(t, err)
			require.Equal(t, int(squareSize*squareSize), len(rawShares))
		})
	}
}

func TestPruning(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	signer := generateKeyringSigner(t, "estimate-key")
	txs := generateManyRawSendTxs(t, encConf.TxConfig, signer, 10)
	txs = append(txs, generateManyRawWirePFD(t, encConf.TxConfig, signer, 10, 1000)...)
	parsedTxs := parseTxs(encConf.TxConfig, txs)
	ss, total := estimateSquareSize(parsedTxs, core.EvidenceList{})
	nextLowestSS := ss / 2
	prunedTxs := prune(encConf.TxConfig, parsedTxs, total, int(nextLowestSS))
	require.Less(t, len(prunedTxs), len(parsedTxs))
}

func Test_compactShareCount(t *testing.T) {
	type test struct {
		name                  string
		normalTxs             int
		wPFDCount, messgeSize int
	}
	tests := []test{
		{"empty block minimum square size", 0, 0, 0},
		{"full block with only txs", 10000, 0, 0},
		{"random small block square size 4", 0, 1, 400},
		{"random small block square size 8", 0, 1, 2000},
		{"random small block w/ 10 nomaml txs square size 4", 10, 1, 2000},
		{"random small block square size 16", 0, 4, 2000},
		{"random medium block square size 32", 0, 50, 2000},
		{"full block max square size", 0, 8000, 100},
		{"overly full block", 0, 80, 100000},
		{"one over the perfect estimation edge case", 10, 1, 300},
	}
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	signer := generateKeyringSigner(t, "estimate-key")
	for _, tt := range tests {
		txs := generateManyRawWirePFD(t, encConf.TxConfig, signer, tt.wPFDCount, tt.messgeSize)
		txs = append(txs, generateManyRawSendTxs(t, encConf.TxConfig, signer, tt.normalTxs)...)

		parsedTxs := parseTxs(encConf.TxConfig, txs)
		squareSize, totalSharesUsed := estimateSquareSize(parsedTxs, core.EvidenceList{})

		if totalSharesUsed > int(squareSize*squareSize) {
			parsedTxs = prune(encConf.TxConfig, parsedTxs, totalSharesUsed, int(squareSize))
		}

		malleated, _, err := malleateTxs(encConf.TxConfig, squareSize, parsedTxs, core.EvidenceList{})
		require.NoError(t, err)

		calculatedTxShareCount := calculateCompactShareCount(parsedTxs, core.EvidenceList{}, int(squareSize))

		txShares := shares.SplitTxs(shares.TxsFromBytes(malleated))
		assert.LessOrEqual(t, len(txShares), calculatedTxShareCount, tt.name)

	}
}

func generateManyRawWirePFD(t *testing.T, txConfig client.TxConfig, signer *types.KeyringSigner, count, size int) [][]byte {
	txs := make([][]byte, count)
	for i := 0; i < count; i++ {
		wpfdTx := generateRawWirePFDTx(
			t,
			txConfig,
			randomValidNamespace(),
			tmrand.Bytes(size),
			signer,
			types.AllSquareSizes(size)...,
		)
		txs[i] = wpfdTx
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

// generateRawWirePFD creates a tx with a single MsgWirePayForData message using the provided namespace and message
func generateRawWirePFDTx(t *testing.T, txConfig client.TxConfig, ns, message []byte, signer *types.KeyringSigner, ks ...uint64) (rawTx []byte) {
	coin := sdk.Coin{
		Denom:  BondDenom,
		Amount: sdk.NewInt(10),
	}

	opts := []types.TxBuilderOption{
		types.SetFeeAmount(sdk.NewCoins(coin)),
		types.SetGasLimit(10000000),
	}

	// create a msg
	msg := generateSignedWirePayForData(t, ns, message, signer, opts, ks...)

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

func randomValidNamespace() namespace.ID {
	for {
		s := tmrand.Bytes(8)
		if bytes.Compare(s, consts.MaxReservedNamespace) > 0 {
			return s
		}
	}
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
