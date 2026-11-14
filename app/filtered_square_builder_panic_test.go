package app

import (
	"bytes"
	"math"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	solomachine "github.com/cosmos/ibc-go/v8/modules/light-clients/06-solomachine"
	"github.com/stretchr/testify/require"
)

// newFillPanicContext returns a context over a store the ante handlers in
// these tests write to, so a test can check which writes survive Fill.
func newFillPanicContext(t *testing.T) (sdk.Context, *storetypes.KVStoreKey) {
	t.Helper()
	key := storetypes.NewKVStoreKey("fill-panic")
	ms := store.NewCommitMultiStore(dbm.NewMemDB(), log.NewNopLogger(), metrics.NewNoOpMetrics())
	ms.MountStoreWithDB(key, storetypes.StoreTypeIAVL, nil)
	require.NoError(t, ms.LoadLatestVersion())
	return sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger()), key
}

func isPFBTx(sdkTx sdk.Tx) bool {
	_, has := hasPFB(sdkTx.GetMsgs())
	return has
}

// TestFillDropsTxWhoseAnteHandlerPanics checks that a tx whose ante handler
// panics is dropped from the proposal without its writes, and that the other
// txs are still kept.
func TestFillDropsTxWhoseAnteHandlerPanics(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	normalTx := newNormalTx(t, txConfig)
	panicNormalTx := newNormalTxWithMemo(t, txConfig, "panic")
	blobTx := newBlobTx(t, txConfig)

	memo := func(sdkTx sdk.Tx) string {
		return sdkTx.(sdk.TxWithMemo).GetMemo()
	}

	tests := []struct {
		name        string
		txs         [][]byte
		shouldPanic func(sdk.Tx) bool
		want        [][]byte
	}{
		{
			name:        "normal tx",
			txs:         [][]byte{panicNormalTx, normalTx},
			shouldPanic: func(sdkTx sdk.Tx) bool { return memo(sdkTx) == "panic" },
			want:        [][]byte{normalTx},
		},
		{
			name:        "blob tx",
			txs:         [][]byte{blobTx, normalTx},
			shouldPanic: isPFBTx,
			want:        [][]byte{normalTx},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, key := newFillPanicContext(t)
			var calls int
			handler := func(ctx sdk.Context, sdkTx sdk.Tx, _ bool) (sdk.Context, error) {
				calls++
				// Write before panicking: the write must not survive.
				ctx.KVStore(key).Set([]byte{byte(calls)}, []byte{1})
				if tc.shouldPanic(sdkTx) {
					panic("ante handler panic")
				}
				return ctx, nil
			}
			fsb, err := NewFilteredSquareBuilder(handler, nil, txConfig, fakeChannelKeeper{}, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
			require.NoError(t, err)

			var kept [][]byte
			require.NotPanics(t, func() { kept = fsb.Fill(ctx, tc.txs, math.MaxInt64) })
			require.Equal(t, tc.want, kept)
			require.Equal(t, len(tc.txs), calls)

			// Only the kept tx's write is left.
			kv := ctx.KVStore(key)
			var written int
			for i := 1; i <= calls; i++ {
				if kv.Has([]byte{byte(i)}) {
					written++
				}
			}
			require.Equal(t, len(tc.want), written)

			_, err = fsb.Build()
			require.NoError(t, err)
		})
	}
}

// newSoloMachineCreateClientTx returns a tx whose decoding panics: the solo
// machine client state has no consensus state, and UnpackInterfaces
// dereferences it.
func newSoloMachineCreateClientTx(t *testing.T, txConfig client.TxConfig) []byte {
	t.Helper()
	signer := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String()
	msg, err := clienttypes.NewMsgCreateClient(
		&solomachine.ClientState{Sequence: 1},
		&solomachine.ConsensusState{Diversifier: "d", Timestamp: 1},
		signer,
	)
	require.NoError(t, err)
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	raw, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = txConfig.TxDecoder()(raw) })
	return raw
}

// TestFillDropsTxWhoseDecodingPanics checks that a tx whose decoding panics
// is dropped from the proposal and the other txs are still kept.
func TestFillDropsTxWhoseDecodingPanics(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	normalTx := newNormalTx(t, txConfig)
	panicTx := newSoloMachineCreateClientTx(t, txConfig)

	alwaysPass := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	}
	fsb, err := NewFilteredSquareBuilder(alwaysPass, nil, txConfig, fakeChannelKeeper{}, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)

	ctx, _ := newFillPanicContext(t)
	var kept [][]byte
	require.NotPanics(t, func() { kept = fsb.Fill(ctx, [][]byte{panicTx, normalTx}, math.MaxInt64) })
	require.Equal(t, [][]byte{normalTx}, kept)
}

// TestRecoverTxDecoderLogsPanic checks that a recovered decoder panic is
// logged as an error, so a decoder regression that drops txs stays visible.
func TestRecoverTxDecoderLogsPanic(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	panicTx := newSoloMachineCreateClientTx(t, txConfig)

	var buf bytes.Buffer
	logger := log.NewLogger(&buf, log.ColorOption(false))
	sdkTx, err := recoverTxDecoder(logger.Error, txConfig.TxDecoder())(panicTx)
	require.ErrorIs(t, err, sdkerrors.ErrPanic)
	require.Nil(t, sdkTx)
	require.Contains(t, buf.String(), "ERR")
	require.Contains(t, buf.String(), "recovered a panic in the tx decoder")
	require.Contains(t, buf.String(), lazyTxHash(panicTx).String())

	// A tx that decodes logs nothing.
	buf.Reset()
	_, err = recoverTxDecoder(logger.Error, txConfig.TxDecoder())(newNormalTx(t, txConfig))
	require.NoError(t, err)
	require.Empty(t, buf.String())
}
