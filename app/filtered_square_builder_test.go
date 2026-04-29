package app

import (
	"math"
	"strings"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	"github.com/celestiaorg/celestia-app/v9/app/encoding"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v9/test/util/blobfactory"
	blobtypes "github.com/celestiaorg/celestia-app/v9/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/celestiaorg/go-square/v4/tx"
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
			name:     "undecodable tx is dropped",
			rawTxs:   [][]byte{[]byte("garbage")},
			wantNorm: 0,
			wantBlob: 0,
			wantPFF:  0,
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

// newNormalTx creates an unsigned MsgSend transaction for testing.
func newNormalTx(t *testing.T, txConfig client.TxConfig) []byte {
	t.Helper()
	return newNormalTxWithMemo(t, txConfig, "")
}

// newNormalTxWithMemo creates an unsigned MsgSend tx with the given memo, used
// to control the encoded size of the tx in tests.
func newNormalTxWithMemo(t *testing.T, txConfig client.TxConfig, memo string) []byte {
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
	builder.SetMemo(memo)
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return txBytes
}

// newBlobTx creates a wire-valid BlobTx whose inner SDK tx wraps a real
// MsgPayForBlobs (rather than the nil inner tx used by
// blobfactory.UnsignedBlobTx).
func newBlobTx(t *testing.T, txConfig client.TxConfig) []byte {
	t.Helper()
	privKey := secp256k1.GenPrivKey()
	addr := sdk.AccAddress(privKey.PubKey().Address())

	ns := share.MustNewV0Namespace([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	blob, err := share.NewBlob(ns, []byte("test blob"), share.ShareVersionZero, nil)
	require.NoError(t, err)

	msg, err := blobtypes.NewMsgPayForBlobs(addr.String(), appconsts.Version, blob)
	require.NoError(t, err)

	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	rawSdkTx, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)

	blobTxBytes, err := tx.MarshalBlobTx(rawSdkTx, blob)
	require.NoError(t, err)
	return blobTxBytes
}

func TestFilteredSquareBuilderFillMaxTxBytes(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := encConf.TxConfig

	alwaysPass := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	}

	// Three txs of identical size for cumulative max tx bytes tests.
	tx1 := newNormalTx(t, txConfig)
	tx2 := newNormalTx(t, txConfig)
	tx3 := newNormalTx(t, txConfig)
	require.Equal(t, len(tx1), len(tx2))
	require.Equal(t, len(tx1), len(tx3))
	normalTxSize := int64(len(tx1))

	// A small and a larger tx for the "continue, not break" test: a high-priority
	// tx that exceeds max tx bytes should be skipped, but a smaller subsequent
	// tx should still fit.
	smallTx := newNormalTx(t, txConfig)
	largeTx := newNormalTxWithMemo(t, txConfig, strings.Repeat("a", 1024))
	require.Less(t, len(smallTx), len(largeTx))

	// A blob tx — the full marshaled size (inner tx + blob data) must be the
	// number used for the byte budget, not just the inner tx size.
	blobTx := blobfactory.UnsignedBlobTx(t)
	blobTxSize := int64(len(blobTx))

	// A blob tx with a real MsgPayForBlobs inside (non-nil inner SDK tx).
	signedBlobTx := newBlobTx(t, txConfig)
	signedBlobTxSize := int64(len(signedBlobTx))

	tests := []struct {
		name       string
		txs        [][]byte
		maxTxBytes int64
		wantKept   int
	}{
		{
			name:       "all txs fit when max tx bytes is unbounded",
			txs:        [][]byte{tx1, tx2, tx3},
			maxTxBytes: math.MaxInt64,
			wantKept:   3,
		},
		{
			name:       "first tx alone exceeds max tx bytes",
			txs:        [][]byte{tx1, tx2, tx3},
			maxTxBytes: normalTxSize - 1,
			wantKept:   0,
		},
		{
			name:       "max tx bytes fits exactly one tx",
			txs:        [][]byte{tx1, tx2, tx3},
			maxTxBytes: normalTxSize,
			wantKept:   1,
		},
		{
			name:       "max tx bytes fits exactly two txs",
			txs:        [][]byte{tx1, tx2, tx3},
			maxTxBytes: 2 * normalTxSize,
			wantKept:   2,
		},
		{
			name:       "smaller later tx fits after larger one is skipped",
			txs:        [][]byte{largeTx, smallTx},
			maxTxBytes: int64(len(smallTx)),
			wantKept:   1,
		},
		{
			name:       "blob tx size counted, not just inner tx",
			txs:        [][]byte{blobTx},
			maxTxBytes: blobTxSize - 1,
			wantKept:   0,
		},
		{
			name:       "normal and blob tx share the same max tx bytes",
			txs:        [][]byte{tx1, blobTx},
			maxTxBytes: normalTxSize + blobTxSize - 1,
			wantKept:   1,
		},
		{
			name:       "blob tx with inner SDK tx counted at full marshaled size",
			txs:        [][]byte{signedBlobTx},
			maxTxBytes: signedBlobTxSize - 1,
			wantKept:   0,
		},
		{
			name:       "blob tx with real inner SDK tx fits when max tx bytes allows it",
			txs:        [][]byte{signedBlobTx},
			maxTxBytes: signedBlobTxSize,
			wantKept:   1,
		},
	}

	// Use a max square size large enough that the square is never the limiting
	// factor — only max tx bytes is. This mirrors the scenario the byte filter
	// is meant to guard against: gov max square size much larger than the
	// configured block max bytes.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fsb, err := NewFilteredSquareBuilder(alwaysPass, txConfig, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
			require.NoError(t, err)

			db := dbm.NewMemDB()
			ms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
			ctx := sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger())

			kept := fsb.Fill(ctx, tc.txs, tc.maxTxBytes)
			require.Len(t, kept, tc.wantKept)
		})
	}
}
