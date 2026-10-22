package app

import (
	"math"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

// Field numbers of TxRaw.body_bytes, TxBody.messages and Any.type_url.
const (
	txRawBodyField  = 1
	txBodyMsgsField = 1
	anyTypeURLField = 1
)

func TestFirstMsgIsPayForFibre(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig

	pffTx := blobfactory.UnsignedPayForFibreTx(t, txConfig)
	emptyBodyTx := newUnsignedTx(t, txConfig)
	sendTx := newNormalTx(t, txConfig)
	var pffRaw txtypes.TxRaw
	require.NoError(t, pffRaw.Unmarshal(pffTx))
	pffBody := pffRaw.BodyBytes
	pffMsg := blobfactory.NewMsgPayForFibre(t, secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey), "test")
	bodyField := func(body []byte) []byte {
		return protowire.AppendBytes(protowire.AppendTag(nil, txRawBodyField, protowire.BytesType), body)
	}
	anyURLs := func(urls ...string) []byte {
		var a []byte
		for _, u := range urls {
			a = protowire.AppendBytes(protowire.AppendTag(a, anyTypeURLField, protowire.BytesType), []byte(u))
		}
		return bodyField(protowire.AppendBytes(protowire.AppendTag(nil, txBodyMsgsField, protowire.BytesType), a))
	}
	sendURL := sdk.MsgTypeURL(&banktypes.MsgSend{})

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
		// A decoder keeps the last type URL of an Any.
		{"type URL MsgSend then pay-for-fibre", anyURLs(sendURL, payForFibreTypeURL), true, true},
		{"type URL pay-for-fibre then MsgSend", anyURLs(payForFibreTypeURL, sendURL), false, true},
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

// TestFillStopsSDKDecodesAtCap pins that SDK txs are decoded only by the SDK loop, and only
// until the SDK cap is full.
func TestFillStopsSDKDecodesAtCap(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	txs := make([][]byte, 2*appconsts.MaxSDKMessages+50)
	for i := range txs {
		txs[i] = newNormalTx(t, txConfig)
	}

	var calls int
	normal, _, _, _ := separateTxsDecoded(log.NewNopLogger(), countingTxConfig{txConfig, &calls}, nil, txs)
	require.Equal(t, txs, normal)
	require.Zero(t, calls)

	kept := newTestFill(t, countingTxConfig{txConfig, &calls}, txs)
	require.Equal(t, txs[:appconsts.MaxSDKMessages], kept)
	require.Equal(t, appconsts.MaxSDKMessages, calls)
}

// TestFillDropsLazySDKTxs pins that the SDK loop drops what separateTxsDecoded drops on a full decode.
func TestFillDropsLazySDKTxs(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	ns := share.MustNewV0Namespace([]byte("lazy"))
	blob, err := share.NewBlob(ns, []byte("blob"), share.ShareVersionZero, nil)
	require.NoError(t, err)
	pfb, err := blobtypes.NewMsgPayForBlobs(newMsgSend().FromAddress, appconsts.Version, blob)
	require.NoError(t, err)
	pff := blobfactory.NewMsgPayForFibre(t, secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey), "test")
	field := func(num protowire.Number, v []byte) []byte {
		return protowire.AppendBytes(protowire.AppendTag(nil, num, protowire.BytesType), v)
	}
	unknownMsgTx := field(txRawBodyField, field(txBodyMsgsField, field(anyTypeURLField, []byte("/unknown.Msg"))))

	send := newNormalTx(t, txConfig)
	txs := [][]byte{
		unknownMsgTx,
		newUnsignedTx(t, txConfig, pfb),
		newUnsignedTx(t, txConfig, newMsgSend(), pff),
		send,
	}
	for _, tx := range txs[:3] {
		isPFF, ok := firstMsgIsPayForFibre(tx)
		require.True(t, ok)
		require.False(t, isPFF)
	}
	require.Equal(t, [][]byte{send}, newTestFill(t, txConfig, txs))
}

// newTestFill runs Fill with an ante handler that accepts every tx.
func newTestFill(t *testing.T, txConfig client.TxConfig, txs [][]byte) [][]byte {
	t.Helper()
	alwaysPass := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
	fsb, err := NewFilteredSquareBuilder(alwaysPass, nil, txConfig, fakeChannelKeeper{}, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)
	ms := store.NewCommitMultiStore(dbm.NewMemDB(), log.NewNopLogger(), metrics.NewNoOpMetrics())
	ctx := sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger())
	return fsb.Fill(ctx, txs, math.MaxInt64)
}

// TestFillKeepsSmallerTxAfterOverflow pins that a tx overflowing the cap does not end the loop.
func TestFillKeepsSmallerTxAfterOverflow(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	txs := make([][]byte, 0, appconsts.MaxSDKMessages+1)
	for range appconsts.MaxSDKMessages - 1 {
		txs = append(txs, newNormalTx(t, txConfig))
	}
	twoMsgTx := newUnsignedTx(t, txConfig, newMsgSend(), newMsgSend())
	last := newNormalTx(t, txConfig)
	txs = append(txs, twoMsgTx, last)

	kept := newTestFill(t, txConfig, txs)
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
