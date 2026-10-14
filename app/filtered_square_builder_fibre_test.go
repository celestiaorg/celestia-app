package app

import (
	"bytes"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/test/util/blobfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
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

func TestSeparateTxsFibre(t *testing.T) {
	encConf := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := encConf.TxConfig

	normalTx := newNormalTx(t, txConfig)
	blobTx := blobfactory.UnsignedBlobTx(t)
	payForFibreTx := blobfactory.UnsignedPayForFibreTx(t, txConfig)
	multiPayForFibreTx := newMultiPayForFibreTx(t, txConfig)
	mixedPayForFibreTx := newMixedPayForFibreTx(t, txConfig)
	reservedNamespacePayForFibreTx := newPayForFibreTxWithNamespace(t, txConfig, share.TxNamespace.Bytes())
	unsupportedBlobVersionPayForFibreTx := newPayForFibreTx(t, txConfig, func(msg *fibretypes.MsgPayForFibre) {
		msg.PaymentPromise.BlobVersion = 999
	})

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
		{
			name:     "tx promising a reserved namespace is dropped",
			rawTxs:   [][]byte{reservedNamespacePayForFibreTx},
			wantNorm: 0,
			wantBlob: 0,
			wantPFF:  0,
		},
		{
			name:     "tx promising an unsupported blob version is dropped",
			rawTxs:   [][]byte{unsupportedBlobVersionPayForFibreTx},
			wantNorm: 0,
			wantBlob: 0,
			wantPFF:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			normalTxs, blobTxs, rawBlobTxs, payForFibreTxs := separateTxsDecoded(log.NewNopLogger(), txConfig, nil, tc.rawTxs)
			require.Len(t, normalTxs, tc.wantNorm)
			require.Len(t, blobTxs, tc.wantBlob)
			require.Len(t, rawBlobTxs, tc.wantBlob)
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
			fsb, err := NewFilteredSquareBuilder(tc.anteHandler, nil, txConfig, fakeChannelKeeper{}, 64, 64)
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

	fsb, err := NewFilteredSquareBuilder(alwaysPass, nil, txConfig, fakeChannelKeeper{}, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)

	db := dbm.NewMemDB()
	ms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	ctx := sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger())

	kept := fsb.Fill(ctx, pffTxs, math.MaxInt64)
	require.Len(t, kept, appconsts.MaxPayForFibreMessages)
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

// TestFilteredSquareBuilderFillRollback checks that a tx dropped by the square,
// the ante handler or pay-for-fibre settlement leaves no state or square
// contents behind, and that the txs after it are still kept.
func TestFilteredSquareBuilderFillRollback(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	storeKey := storetypes.NewKVStoreKey("test")

	// The fake ante handler bumps a per-tx "sequence" and the fake router
	// writes a per-tx "escrow" debit, both keyed by the SDK tx bytes.
	seqKey := func(txBytes []byte) []byte { return append([]byte("seq/"), txBytes...) }
	escrowKey := func(txBytes []byte) []byte { return append([]byte("escrow/"), txBytes...) }

	norm1, norm2, norm3 := newNormalTx(t, txConfig), newNormalTx(t, txConfig), newNormalTx(t, txConfig)
	midNorm := newNormalTxWithMemo(t, txConfig, strings.Repeat("a", 1000)) // 3 compact shares
	bigNorm := newNormalTxWithMemo(t, txConfig, strings.Repeat("a", 2500)) // larger than a 2x2 square
	blob1, blob2, blob3 := newBlobTx(t, txConfig), newBlobTx(t, txConfig), newBlobTx(t, txConfig)
	pff1, pff2, pff3 := blobfactory.UnsignedPayForFibreTx(t, txConfig), blobfactory.UnsignedPayForFibreTx(t, txConfig), blobfactory.UnsignedPayForFibreTx(t, txConfig)
	deposit1, deposit2 := newDepositToEscrowTx(t, txConfig), newDepositToEscrowTx(t, txConfig)

	tests := []struct {
		name       string
		squareSize int
		txs        [][]byte
		anteReject [][]byte
		// anteWriteReject txs bump their sequence before the ante handler
		// rejects them.
		anteWriteReject [][]byte
		settleErr       [][]byte
		settleOOG       [][]byte
		wantKept        [][]byte
		wantEscrows     [][]byte
	}{
		{
			name:        "keeps every tx in type order",
			squareSize:  64,
			txs:         [][]byte{pff1, blob1, norm1, pff2, blob2, norm2},
			wantKept:    [][]byte{norm1, norm2, blob1, blob2, pff1, pff2},
			wantEscrows: [][]byte{pff1, pff2},
		},
		{
			name:        "settlement error rolls back the pay-for-fibre and keeps the next one",
			squareSize:  64,
			txs:         [][]byte{pff1, pff2, pff3},
			settleErr:   [][]byte{pff2},
			wantKept:    [][]byte{pff1, pff3},
			wantEscrows: [][]byte{pff1, pff3},
		},
		{
			name:        "settlement out of gas rolls back the pay-for-fibre and keeps the next one",
			squareSize:  64,
			txs:         [][]byte{pff1, pff2, pff3},
			settleOOG:   [][]byte{pff2},
			wantKept:    [][]byte{pff1, pff3},
			wantEscrows: [][]byte{pff1, pff3},
		},
		{
			name:        "first pay-for-fibre fails settlement",
			squareSize:  64,
			txs:         [][]byte{pff1, pff2},
			settleErr:   [][]byte{pff1},
			wantKept:    [][]byte{pff2},
			wantEscrows: [][]byte{pff2},
		},
		{
			name:        "ante rejection drops the tx of each type and keeps the next one",
			squareSize:  64,
			txs:         [][]byte{norm1, norm2, norm3, blob1, blob2, blob3, pff1, pff2, pff3},
			anteReject:  [][]byte{norm2, blob2, pff2},
			wantKept:    [][]byte{norm1, norm3, blob1, blob3, pff1, pff3},
			wantEscrows: [][]byte{pff1, pff3},
		},
		{
			name:            "ante write before rejection is discarded for pay-for-fibre",
			squareSize:      64,
			txs:             [][]byte{pff1, pff2, pff3},
			anteWriteReject: [][]byte{pff2},
			wantKept:        [][]byte{pff1, pff3},
			wantEscrows:     [][]byte{pff1, pff3},
		},
		{
			name:        "consecutive settlement and ante failures",
			squareSize:  64,
			txs:         [][]byte{pff1, pff2, pff3},
			settleErr:   [][]byte{pff1},
			anteReject:  [][]byte{pff2},
			wantKept:    [][]byte{pff3},
			wantEscrows: [][]byte{pff3},
		},
		{
			name:       "full square skips each tx type that does not fit",
			squareSize: 2,
			txs:        [][]byte{bigNorm, midNorm, blob1, pff1},
			wantKept:   [][]byte{midNorm},
		},
		{
			name:        "failed fibre state msg keeps the tx without its writes",
			squareSize:  64,
			txs:         [][]byte{deposit1, deposit2, pff1},
			settleErr:   [][]byte{deposit1},
			wantKept:    [][]byte{deposit1, deposit2, pff1},
			wantEscrows: [][]byte{deposit2, pff1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// matches reports whether the SDK tx on ctx belongs to one of txs.
			matches := func(txs [][]byte, ctx sdk.Context) bool {
				for _, rawTx := range txs {
					if bytes.Equal(sdkTxBytes(t, rawTx), ctx.TxBytes()) {
						return true
					}
				}
				return false
			}
			ante := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
				if matches(tc.anteReject, ctx) {
					return ctx, errors.New("ante handler rejected tx")
				}
				ctx = ctx.WithGasMeter(storetypes.NewGasMeter(1_000_000))
				ctx.KVStore(storeKey).Set(seqKey(ctx.TxBytes()), []byte{1})
				if matches(tc.anteWriteReject, ctx) {
					return ctx, errors.New("ante handler rejected tx after writing")
				}
				return ctx, nil
			}
			router := stubMsgRouter{handler: func(ctx sdk.Context, _ sdk.Msg) (*sdk.Result, error) {
				ctx.KVStore(storeKey).Set(escrowKey(ctx.TxBytes()), []byte{1})
				if matches(tc.settleErr, ctx) {
					return nil, errors.New("settlement failed")
				}
				if matches(tc.settleOOG, ctx) {
					ctx.GasMeter().ConsumeGas(ctx.GasMeter().Limit()+1, "settlement")
				}
				return &sdk.Result{}, nil
			}}

			db := dbm.NewMemDB()
			cms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
			cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
			require.NoError(t, cms.LoadLatestVersion())
			ctx := sdk.NewContext(cms, cmtproto.Header{}, false, log.NewNopLogger())

			fsb, err := NewFilteredSquareBuilder(ante, router, txConfig, fakeChannelKeeper{}, tc.squareSize, appconsts.SubtreeRootThreshold)
			require.NoError(t, err)
			kept := fsb.Fill(ctx, tc.txs, math.MaxInt64)
			require.Equal(t, tc.wantKept, kept)

			// Only kept txs leave a sequence bump, and only settled ones an escrow write.
			kv := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).KVStore(storeKey)
			for i, rawTx := range tc.txs {
				txBytes := sdkTxBytes(t, rawTx)
				require.Equal(t, slices.ContainsFunc(tc.wantKept, func(b []byte) bool { return bytes.Equal(b, rawTx) }), kv.Has(seqKey(txBytes)), "sequence of tx %d", i)
				require.Equal(t, slices.ContainsFunc(tc.wantEscrows, func(b []byte) bool { return bytes.Equal(b, rawTx) }), kv.Has(escrowKey(txBytes)), "escrow of tx %d", i)
			}

			// The square must match one built from only the kept txs, so no
			// dropped tx or system blob is left behind.
			got, err := fsb.Build()
			require.NoError(t, err)
			alwaysPass := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
			ref, err := NewFilteredSquareBuilder(alwaysPass, nil, txConfig, fakeChannelKeeper{}, tc.squareSize, appconsts.SubtreeRootThreshold)
			require.NoError(t, err)
			require.Equal(t, tc.wantKept, ref.Fill(ctx, tc.wantKept, math.MaxInt64))
			want, err := ref.Build()
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}

// newDepositToEscrowTx creates an unsigned MsgDepositToEscrow tx.
func newDepositToEscrowTx(t *testing.T, txConfig client.TxConfig) []byte {
	t.Helper()
	addr := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(&fibretypes.MsgDepositToEscrow{Signer: addr.String(), Amount: sdk.NewInt64Coin("utia", 1)}))
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return txBytes
}

// sdkTxBytes returns the SDK tx bytes Fill sets on the context: the inner tx
// of a blob tx, otherwise the raw tx.
func sdkTxBytes(t *testing.T, rawTx []byte) []byte {
	t.Helper()
	bTx, isBlob, err := tx.UnmarshalBlobTx(rawTx)
	if !isBlob {
		return rawTx
	}
	require.NoError(t, err)
	return bTx.Tx
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
