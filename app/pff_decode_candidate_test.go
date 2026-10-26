package app

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
)

// TestPFFDecodeCandidateAgreesWithIsPayForFibreTx pins the fast path against
// IsPayForFibreTx; they differ only on malformed MsgPayForFibre bytes, rejected anyway.
func TestPFFDecodeCandidateAgreesWithIsPayForFibreTx(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	decode := txConfig.TxDecoder()

	pff := newPayForFibreTx(t, txConfig, func(*fibretypes.MsgPayForFibre) {})

	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(fibretypes.PayForFibreMsg(decodeTx(t, txConfig, pff))))
	builder.SetMemo("memo")
	pffWithMemo, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)

	_, fast := singleMessageTypeURL(pff)
	require.True(t, fast, "a plain pay-for-fibre tx must take the fast path")
	_, fast = singleMessageTypeURL(pffWithMemo)
	require.False(t, fast, "a memo must send the tx to IsPayForFibreTx")

	rawTx := func(messages ...*codectypes.Any) []byte {
		body, err := (&txtypes.TxBody{Messages: messages}).Marshal()
		require.NoError(t, err)
		raw, err := (&txtypes.TxRaw{BodyBytes: body}).Marshal()
		require.NoError(t, err)
		return raw
	}
	malformedPFF := rawTx(&codectypes.Any{TypeUrl: fibretypes.MsgPayForFibreTypeURL, Value: []byte{0xff}})

	blob, err := share.NewBlob(share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize)), []byte("data"), share.ShareVersionZero, nil)
	require.NoError(t, err)
	blobWrappingPFF, err := blobtx.MarshalBlobTx(pff, blob)
	require.NoError(t, err)
	indexWrappedPFF, err := blobtx.MarshalIndexWrapper(pff, 0)
	require.NoError(t, err)

	tests := []struct {
		name string
		raw  []byte
		want bool
	}{
		{"pay-for-fibre", pff, true},
		{"pay-for-fibre with memo (fallback path)", pffWithMemo, true},
		{"send", newMultiMsgSendTx(t, txConfig, 1), false},
		{"two sends", newMultiMsgSendTx(t, txConfig, 2), false},
		{"two pay-for-fibre messages", rawTx(
			&codectypes.Any{TypeUrl: fibretypes.MsgPayForFibreTypeURL},
			&codectypes.Any{TypeUrl: fibretypes.MsgPayForFibreTypeURL},
		), false},
		{"blob tx wrapping a pay-for-fibre tx", blobWrappingPFF, false},
		{"index wrapper around a pay-for-fibre tx", indexWrappedPFF, false},
		{"garbage", []byte{0x0a, 0xff, 0xff}, false},
		{"empty", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, fibretypes.IsPayForFibreTx(tc.raw))
			require.Equal(t, tc.want, pffDecodeCandidate(tc.raw))
		})
	}

	t.Run("malformed MsgPayForFibre", func(t *testing.T) {
		require.False(t, fibretypes.IsPayForFibreTx(malformedPFF))
		require.True(t, pffDecodeCandidate(malformedPFF))
		_, err := decode(malformedPFF)
		require.Error(t, err, "the walk must reject what only the fast path counts")
	})
}
