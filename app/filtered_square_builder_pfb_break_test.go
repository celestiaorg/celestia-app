package app

import (
	"math"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestFillStopsPFBDecodesAtCap pins that the blob loop decodes no inner tx once the PFB cap is full.
func TestFillStopsPFBDecodesAtCap(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	txs := make([][]byte, 2*appconsts.MaxPFBMessages+50)
	for i := range txs {
		txs[i] = newBlobTx(t, txConfig)
	}

	var calls int
	kept := newTestBlobFill(t, countingTxConfig{txConfig, &calls}, txConfig, txs)
	require.Equal(t, txs[:appconsts.MaxPFBMessages], kept)
	require.Equal(t, appconsts.MaxPFBMessages, calls)
}

// TestValidateBlobTxKeepsOnlySinglePFB pins the premise of the blob loop break: every blob tx
// Fill keeps weighs exactly one PFB message.
func TestValidateBlobTxKeepsOnlySinglePFB(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	tests := []struct {
		name    string
		rawTx   []byte
		wantErr bool
	}{
		{"one PFB", newBlobTx(t, txConfig), false},
		{"no messages", newBlobTxWithMsgs(t, txConfig, 0), true},
		{"two PFBs", newBlobTxWithMsgs(t, txConfig, 2), true},
		{"MsgSend", newBlobTxWithInner(t, newUnsignedTx(t, txConfig, newMsgSend())), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bTx, isBlob, err := blobtx.UnmarshalBlobTx(tc.rawTx)
			require.True(t, isBlob)
			require.NoError(t, err)
			err = blobtypes.ValidateBlobTx(txConfig, bTx, appconsts.SubtreeRootThreshold, appconsts.Version)
			require.Equal(t, tc.wantErr, err != nil, err)
		})
	}
}

// TestFillKeepsSmallerBlobTxAfterOverflow pins that a blob tx overflowing the cap does not end
// the loop. Only reachable without validateBlobTx, which rejects multi-PFB blob txs.
func TestFillKeepsSmallerBlobTxAfterOverflow(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	txs := make([][]byte, 0, appconsts.MaxPFBMessages+1)
	for range appconsts.MaxPFBMessages - 1 {
		txs = append(txs, newBlobTx(t, txConfig))
	}
	twoPFBTx := newBlobTxWithMsgs(t, txConfig, 2)
	last := newBlobTx(t, txConfig)
	txs = append(txs, twoPFBTx, last)

	kept := newTestFill(t, txConfig, txs)
	require.Len(t, kept, appconsts.MaxPFBMessages)
	require.Equal(t, last, kept[len(kept)-1])
	require.NotContains(t, kept, twoPFBTx)
}

// newTestBlobFill runs Fill with an ante handler that accepts every tx and the production
// blob tx validation, decoding with validationConfig so dec's decoder sees only Fill's calls.
func newTestBlobFill(t *testing.T, txConfig, validationConfig client.TxConfig, txs [][]byte) [][]byte {
	t.Helper()
	alwaysPass := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
	fsb, err := NewFilteredSquareBuilder(alwaysPass, nil, txConfig, fakeChannelKeeper{}, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)
	fsb.validateBlobTx = func(bTx *blobtx.BlobTx) (bool, error) {
		return false, blobtypes.ValidateBlobTx(validationConfig, bTx, appconsts.SubtreeRootThreshold, appconsts.Version)
	}
	ms := store.NewCommitMultiStore(dbm.NewMemDB(), log.NewNopLogger(), metrics.NewNoOpMetrics())
	ctx := sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger())
	return fsb.Fill(ctx, txs, math.MaxInt64)
}

// newBlobTxWithMsgs returns a blob tx whose inner tx holds n PFBs for its blob.
func newBlobTxWithMsgs(t *testing.T, txConfig client.TxConfig, n int) []byte {
	t.Helper()
	addr := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	msgs := make([]sdk.Msg, n)
	for i := range msgs {
		var err error
		msgs[i], err = blobtypes.NewMsgPayForBlobs(addr.String(), appconsts.Version, pfbBreakBlob(t))
		require.NoError(t, err)
	}
	return newBlobTxWithInner(t, newUnsignedTx(t, txConfig, msgs...))
}

func newBlobTxWithInner(t *testing.T, inner []byte) []byte {
	t.Helper()
	bTx, err := blobtx.MarshalBlobTx(inner, pfbBreakBlob(t))
	require.NoError(t, err)
	return bTx
}

func pfbBreakBlob(t *testing.T) *share.Blob {
	t.Helper()
	blob, err := share.NewBlob(share.MustNewV0Namespace([]byte("pfb-break")), []byte("blob"), share.ShareVersionZero, nil)
	require.NoError(t, err)
	return blob
}
