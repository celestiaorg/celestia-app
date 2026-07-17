package app

import (
	"errors"
	"math"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/test/util/blobfactory"
	"github.com/celestiaorg/go-square/v4/share"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestSeparateTxsFibre(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := encConf.TxConfig

	normalTx := newNormalTx(t, txConfig)
	blobTx := blobfactory.UnsignedBlobTx(t)
	payForFibreTx := blobfactory.UnsignedPayForFibreTx(t, txConfig)
	multiPayForFibreTx := newMultiPayForFibreTx(t, txConfig)
	mixedPayForFibreTx := newMixedPayForFibreTx(t, txConfig)

	tests := []struct {
		name     string
		rawTxs   [][]byte
		wantNorm int
		wantBlob int
		wantPFF  int
	}{
		{
			name:     "one pay-for-fibre tx",
			rawTxs:   [][]byte{payForFibreTx},
			wantNorm: 0,
			wantBlob: 0,
			wantPFF:  1,
		},
		{
			name:     "one of each",
			rawTxs:   [][]byte{normalTx, blobTx, payForFibreTx},
			wantNorm: 1,
			wantBlob: 1,
			wantPFF:  1,
		},
		{
			name:     "two pay-for-fibre txs",
			rawTxs:   [][]byte{payForFibreTx, payForFibreTx},
			wantNorm: 0,
			wantBlob: 0,
			wantPFF:  2,
		},
		{
			name:     "tx with multiple MsgPayForFibre is dropped",
			rawTxs:   [][]byte{multiPayForFibreTx},
			wantNorm: 0,
			wantBlob: 0,
			wantPFF:  0,
		},
		{
			name:     "tx with MsgPayForFibre mixed with MsgSend is dropped",
			rawTxs:   [][]byte{mixedPayForFibreTx},
			wantNorm: 0,
			wantBlob: 0,
			wantPFF:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			normalTxs, blobTxs, payForFibreTxs := separateTxs(log.NewNopLogger(), txConfig, tc.rawTxs)
			require.Len(t, normalTxs, tc.wantNorm)
			require.Len(t, blobTxs, tc.wantBlob)
			require.Len(t, payForFibreTxs, tc.wantPFF)
		})
	}
}

func TestCountMsgPayForFibre(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := encConf.TxConfig

	tests := []struct {
		name      string
		txBytes   func() []byte
		wantCount int
	}{
		{
			name:      "MsgPayForFibre",
			txBytes:   func() []byte { return blobfactory.UnsignedPayForFibreTx(t, txConfig) },
			wantCount: 1,
		},
		{
			name:      "MsgSend",
			txBytes:   func() []byte { return newNormalTx(t, txConfig) },
			wantCount: 0,
		},
		{
			name:      "two MsgPayForFibre",
			txBytes:   func() []byte { return newMultiPayForFibreTx(t, txConfig) },
			wantCount: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sdkTx, err := txConfig.TxDecoder()(tc.txBytes())
			require.NoError(t, err)

			count := countMsgPayForFibre(sdkTx)
			require.Equal(t, tc.wantCount, count)
		})
	}
}

func TestFilteredSquareBuilderFillWithPayForFibre(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := encConf.TxConfig

	alwaysPass := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	}
	alwaysReject := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, errors.New("ante handler rejected tx")
	}

	normalTx := newNormalTx(t, txConfig)
	// Use a blob tx with a real MsgPayForBlobs inner SDK tx (not the nil inner
	// tx from blobfactory.UnsignedBlobTx) so it survives the inner-tx decode in
	// Fill, where the decoder rejects empty tx bytes.
	blobTx := newBlobTx(t, txConfig)
	payForFibreTx := blobfactory.UnsignedPayForFibreTx(t, txConfig)

	tests := []struct {
		name              string
		anteHandler       sdk.AnteHandler
		txs               [][]byte
		wantKeptCount     int
		wantPFFCount      int // plain SDK MsgPayForFibre outputs (stable hash)
		wantFibreInSquare bool
	}{
		{
			name:              "only pay-for-fibre",
			anteHandler:       alwaysPass,
			txs:               [][]byte{payForFibreTx},
			wantKeptCount:     1,
			wantPFFCount:      1,
			wantFibreInSquare: true,
		},
		{
			name:              "normal and pay-for-fibre",
			anteHandler:       alwaysPass,
			txs:               [][]byte{normalTx, payForFibreTx},
			wantKeptCount:     2,
			wantPFFCount:      1,
			wantFibreInSquare: true,
		},
		{
			name:              "blob and pay-for-fibre",
			anteHandler:       alwaysPass,
			txs:               [][]byte{blobTx, payForFibreTx},
			wantKeptCount:     2,
			wantPFFCount:      1,
			wantFibreInSquare: true,
		},
		{
			name:              "all three types",
			anteHandler:       alwaysPass,
			txs:               [][]byte{normalTx, blobTx, payForFibreTx},
			wantKeptCount:     3,
			wantPFFCount:      1,
			wantFibreInSquare: true,
		},
		{
			name:              "two pay-for-fibre txs",
			anteHandler:       alwaysPass,
			txs:               [][]byte{payForFibreTx, payForFibreTx},
			wantKeptCount:     2,
			wantPFFCount:      2,
			wantFibreInSquare: true,
		},
		{
			name:              "pay-for-fibre rejected by ante handler is excluded",
			anteHandler:       alwaysReject,
			txs:               [][]byte{payForFibreTx},
			wantKeptCount:     0,
			wantPFFCount:      0,
			wantFibreInSquare: false,
		},
		{
			name:              "all txs rejected when ante handler rejects everything",
			anteHandler:       alwaysReject,
			txs:               [][]byte{normalTx, blobTx, payForFibreTx},
			wantKeptCount:     0,
			wantPFFCount:      0,
			wantFibreInSquare: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fsb, err := NewFilteredSquareBuilder(tc.anteHandler, txConfig, 64, 64)
			require.NoError(t, err)

			db := dbm.NewMemDB()
			ms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
			ctx := sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger())

			kept := fsb.Fill(ctx, tc.txs, math.MaxInt64)
			require.Len(t, kept, tc.wantKeptCount)

			// Count how many of the kept txs are plain SDK MsgPayForFibre txs.
			pffCount := 0
			for _, rawTx := range kept {
				sdkTx, decErr := txConfig.TxDecoder()(rawTx)
				if decErr != nil {
					continue
				}
				if countMsgPayForFibre(sdkTx) > 0 {
					pffCount++
				}
			}
			require.Equal(t, tc.wantPFFCount, pffCount)

			sq, err := fsb.Build()
			require.NoError(t, err)

			pffRange := share.GetShareRangeForNamespace(sq, share.PayForFibreNamespace)
			if tc.wantFibreInSquare {
				require.False(t, pffRange.IsEmpty(), "expected PayForFibreNamespace shares in square")
			} else {
				require.True(t, pffRange.IsEmpty(), "expected no PayForFibreNamespace shares in square")
			}
		})
	}
}

func TestFilteredSquareBuilderFillMaxPayForFibreMessages(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := encConf.TxConfig

	alwaysPass := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	}

	// Create appconsts.MaxPayForFibreMessages+1 pay-for-fibre txs.
	numPFF := appconsts.MaxPayForFibreMessages + 1
	pffTxs := make([][]byte, numPFF)
	for i := range numPFF {
		pffTxs[i] = blobfactory.UnsignedPayForFibreTx(t, txConfig)
	}

	fsb, err := NewFilteredSquareBuilder(alwaysPass, txConfig, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)

	db := dbm.NewMemDB()
	ms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	ctx := sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger())

	kept := fsb.Fill(ctx, pffTxs, math.MaxInt64)
	require.Len(t, kept, appconsts.MaxPayForFibreMessages)
}

// TestFilteredSquareBuilderFillDropsIndexWrappedFibreTx ensures an
// IndexWrapper-encoded MsgPayForFibre is dropped rather than crashing the
// builder. separateTxs classifies it as a fibre tx (the unwrapping decoder sees
// the inner MsgPayForFibre), but TryParseFibreTx parses the raw wrapper bytes to
// nil; without the nil guard this panics in AppendFibreTx and halts the chain.
func TestFilteredSquareBuilderFillDropsIndexWrappedFibreTx(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := encConf.TxConfig

	alwaysPass := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	}

	payForFibreTx := blobfactory.UnsignedPayForFibreTx(t, txConfig)
	wrapped, err := coretypes.MarshalIndexWrapper(payForFibreTx, 0)
	require.NoError(t, err)

	// The wrapper is classified as a fibre tx, which is what feeds the crash path.
	_, _, payForFibreTxs := separateTxs(log.NewNopLogger(), txConfig, [][]byte{wrapped})
	require.Len(t, payForFibreTxs, 1)

	fsb, err := NewFilteredSquareBuilder(alwaysPass, txConfig, 64, 64)
	require.NoError(t, err)

	db := dbm.NewMemDB()
	ms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	ctx := sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger())

	// Must not panic; the wrapped tx is dropped and no fibre shares are produced.
	kept := fsb.Fill(ctx, [][]byte{wrapped}, math.MaxInt64)
	require.Empty(t, kept)

	sq, err := fsb.Build()
	require.NoError(t, err)
	require.True(t, share.GetShareRangeForNamespace(sq, share.PayForFibreNamespace).IsEmpty())
}

// newMultiPayForFibreTx creates an unsigned SDK tx containing two MsgPayForFibre messages for testing.
func newMultiPayForFibreTx(t *testing.T, txConfig client.TxConfig) []byte {
	t.Helper()
	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey().(*secp256k1.PubKey)
	msg1 := blobfactory.NewMsgPayForFibre(t, pubKey, "test")
	msg2 := blobfactory.NewMsgPayForFibre(t, pubKey, "test")
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg1, msg2))
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return txBytes
}

// newMixedPayForFibreTx creates an unsigned SDK tx containing one MsgPayForFibre
// and one MsgSend for testing.
func newMixedPayForFibreTx(t *testing.T, txConfig client.TxConfig) []byte {
	t.Helper()
	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey().(*secp256k1.PubKey)
	addr := sdk.AccAddress(pubKey.Address())
	pffMsg := blobfactory.NewMsgPayForFibre(t, pubKey, "test")
	sendMsg := &banktypes.MsgSend{
		FromAddress: addr.String(),
		ToAddress:   addr.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("utia", 1)),
	}
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(pffMsg, sendMsg))
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return txBytes
}
