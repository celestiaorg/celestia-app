package app

import (
	"math"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/test/util/blobfactory"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestFirstMsgIsPayForFibre(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig

	pffTx := blobfactory.UnsignedPayForFibreTx(t, txConfig)
	emptyBodyTx := newUnsignedTx(t, txConfig)
	sendTx := newNormalTx(t, txConfig)
	pffBody, ok := protoBytesField(pffTx, txRawBodyField, false)
	require.True(t, ok)
	pffMsg := blobfactory.NewMsgPayForFibre(t, secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey), "test")
	bodyField := func(body []byte) []byte {
		return protowire.AppendBytes(protowire.AppendTag(nil, txRawBodyField, protowire.BytesType), body)
	}

	tests := []struct {
		name      string
		rawTx     []byte
		wantPFF   bool
		wantParse bool
	}{
		{"pay-for-fibre", pffTx, true, true},
		{"MsgSend", sendTx, false, true},
		{"pay-for-fibre then MsgSend", newMixedPayForFibreTx(t, txConfig), true, true},
		{"two pay-for-fibre", newMultiPayForFibreTx(t, txConfig), true, true},
		{"MsgSend then pay-for-fibre", newUnsignedTx(t, txConfig, newMsgSend(), pffMsg), false, true},
		{"empty body", emptyBodyTx, false, true},
		// A decoder keeps the last body, so an earlier one must not decide the class.
		{"pay-for-fibre body then MsgSend body", append(bodyField(pffBody), sendTx...), false, true},
		{"empty bytes", nil, false, true},
		{"garbage", []byte("garbage"), false, false},
		{"truncated", pffTx[:len(pffTx)/2], false, false},
		{"malformed body", bodyField([]byte("garbage")), false, false},
		{"group in body", bodyField(protowire.AppendTag(nil, 2, protowire.StartGroupType)), false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isPFF, ok := firstMsgIsPayForFibre(tc.rawTx)
			require.Equal(t, tc.wantParse, ok)
			require.Equal(t, tc.wantPFF, isPFF)
		})
	}
}

// countingTxConfig counts the txs its decoder is asked to decode.
type countingTxConfig struct {
	client.TxConfig
	calls *int
}

func (c countingTxConfig) TxDecoder() sdk.TxDecoder {
	dec := c.TxConfig.TxDecoder()
	return func(bz []byte) (sdk.Tx, error) {
		*c.calls++
		return dec(bz)
	}
}

// TestCandidateBoundsBoundDecoding pins that an over-cap workload decodes at most the bounds.
func TestCandidateBoundsBoundDecoding(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig

	const extra = 50
	sdkTxs := make([][]byte, appconsts.MaxSDKCandidates+extra)
	for i := range sdkTxs {
		sdkTxs[i] = newNormalTx(t, txConfig)
	}
	blobTxs := make([][]byte, appconsts.MaxPFBCandidates+extra)
	for i := range blobTxs {
		blobTxs[i] = newBlobTx(t, txConfig)
	}
	pffTxs := make([][]byte, appconsts.MaxPayForFibreCandidates+extra)
	for i := range pffTxs {
		pffTxs[i] = blobfactory.UnsignedPayForFibreTx(t, txConfig)
	}
	all := append(append(append([][]byte{}, sdkTxs...), blobTxs...), pffTxs...)

	t.Run("separateTxsDecoded", func(t *testing.T) {
		var calls int
		normal, blobs, _, pffs := separateTxsDecoded(log.NewNopLogger(), countingTxConfig{txConfig, &calls}, nil, all)
		require.Equal(t, sdkTxs[:appconsts.MaxSDKCandidates], normal)
		require.Len(t, blobs, appconsts.MaxPFBCandidates)
		require.Len(t, pffs, appconsts.MaxPayForFibreCandidates)
		require.Equal(t, appconsts.MaxSDKCandidates+appconsts.MaxPayForFibreCandidates, calls)
	})

	t.Run("Fill", func(t *testing.T) {
		alwaysPass := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
		var calls int
		fsb, err := NewFilteredSquareBuilder(alwaysPass, nil, countingTxConfig{txConfig, &calls}, fakeChannelKeeper{}, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
		require.NoError(t, err)
		ms := store.NewCommitMultiStore(dbm.NewMemDB(), log.NewNopLogger(), metrics.NewNoOpMetrics())
		ctx := sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger())

		kept := fsb.Fill(ctx, all, math.MaxInt64)
		require.Len(t, kept, appconsts.MaxSDKMessages+appconsts.MaxPFBMessages+appconsts.MaxPayForFibreMessages)
		// The class loops stop decoding once their cap is reached.
		require.Equal(t, appconsts.MaxSDKCandidates+appconsts.MaxPayForFibreCandidates+
			appconsts.MaxSDKMessages+appconsts.MaxPFBMessages, calls)
	})
}

// TestFillKeepsSmallerTxAfterOverflow pins that a tx overflowing the cap does not end the loop.
func TestFillKeepsSmallerTxAfterOverflow(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	alwaysPass := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }

	txs := make([][]byte, 0, appconsts.MaxSDKMessages+1)
	for range appconsts.MaxSDKMessages - 1 {
		txs = append(txs, newNormalTx(t, txConfig))
	}
	twoMsgTx := newUnsignedTx(t, txConfig, newMsgSend(), newMsgSend())
	last := newNormalTx(t, txConfig)
	txs = append(txs, twoMsgTx, last)

	fsb, err := NewFilteredSquareBuilder(alwaysPass, nil, txConfig, fakeChannelKeeper{}, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)
	ms := store.NewCommitMultiStore(dbm.NewMemDB(), log.NewNopLogger(), metrics.NewNoOpMetrics())
	ctx := sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger())

	kept := fsb.Fill(ctx, txs, math.MaxInt64)
	require.Len(t, kept, appconsts.MaxSDKMessages)
	require.Equal(t, last, kept[len(kept)-1])
	require.NotContains(t, kept, twoMsgTx)
}

func newUnsignedTx(t *testing.T, txConfig client.TxConfig, msgs ...sdk.Msg) []byte {
	t.Helper()
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msgs...))
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return txBytes
}

func newMsgSend() *banktypes.MsgSend {
	addr := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String()
	return &banktypes.MsgSend{FromAddress: addr, ToAddress: addr, Amount: sdk.NewCoins(sdk.NewInt64Coin("utia", 1))}
}
