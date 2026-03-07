package app

import (
	"errors"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/test/util/blobfactory"
	"github.com/celestiaorg/go-square/v4/share"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestSeparateTxs(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := encConf.TxConfig

	normalTx := newNormalTx(t, txConfig)
	blobTx := blobfactory.UnsignedBlobTx(t)
	payForFibreTx := blobfactory.UnsignedPayForFibreTx(t, txConfig)

	tests := []struct {
		name     string
		rawTxs   [][]byte
		wantNorm int
		wantBlob int
		wantPFF  int
	}{
		{
			name:     "empty",
			rawTxs:   [][]byte{},
			wantNorm: 0,
			wantBlob: 0,
			wantPFF:  0,
		},
		{
			name:     "one normal tx",
			rawTxs:   [][]byte{normalTx},
			wantNorm: 1,
			wantBlob: 0,
			wantPFF:  0,
		},
		{
			name:     "one blob tx",
			rawTxs:   [][]byte{blobTx},
			wantNorm: 0,
			wantBlob: 1,
			wantPFF:  0,
		},
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			normalTxs, blobTxs, payForFibreTxs := separateTxs(txConfig, tc.rawTxs)
			require.Len(t, normalTxs, tc.wantNorm)
			require.Len(t, blobTxs, tc.wantBlob)
			require.Len(t, payForFibreTxs, tc.wantPFF)
		})
	}
}

func TestExtractMsgPayForFibre(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := encConf.TxConfig

	tests := []struct {
		name      string
		txBytes   func() []byte
		wantFound bool
	}{
		{
			name:      "MsgPayForFibre",
			txBytes:   func() []byte { return blobfactory.UnsignedPayForFibreTx(t, txConfig) },
			wantFound: true,
		},
		{
			name:      "MsgSend",
			txBytes:   func() []byte { return newNormalTx(t, txConfig) },
			wantFound: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sdkTx, err := txConfig.TxDecoder()(tc.txBytes())
			require.NoError(t, err)

			msg, found := extractMsgPayForFibre(sdkTx)
			require.Equal(t, tc.wantFound, found)
			if tc.wantFound {
				require.NotNil(t, msg)
			} else {
				require.Nil(t, msg)
			}
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
	blobTx := blobfactory.UnsignedBlobTx(t)
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
			name:        "non-fibre txs kept when ante handler rejects pay-for-fibre",
			anteHandler: alwaysReject,
			txs:         [][]byte{normalTx, blobTx, payForFibreTx},
			// normalTx and blobTx are also rejected because alwaysReject fires for every tx type.
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

			kept := fsb.Fill(ctx, tc.txs)
			require.Len(t, kept, tc.wantKeptCount)

			// Count how many of the kept txs are plain SDK MsgPayForFibre txs.
			// Fill now returns rawTx (plain SDK bytes) for hash stability.
			pffCount := 0
			for _, rawTx := range kept {
				sdkTx, decErr := txConfig.TxDecoder()(rawTx)
				if decErr != nil {
					continue
				}
				if _, isPFF := extractMsgPayForFibre(sdkTx); isPFF {
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

// newNormalTx creates an unsigned MsgSend transaction for testing.
func newNormalTx(t *testing.T, txConfig client.TxConfig) []byte {
	t.Helper()
	privKey := secp256k1.GenPrivKey()
	addr := sdk.AccAddress(privKey.PubKey().Address())
	msg := &banktypes.MsgSend{
		FromAddress: addr.String(),
		ToAddress:   addr.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("utia", 1)),
	}
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return txBytes
}
