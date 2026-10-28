package app

import (
	"slices"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

// TestFirstMsgIsPayForFibreMatchesDecoder pins the peek to the decoder on decodable txs with
// duplicated fields, which the decoder resolves last-wins.
func TestFirstMsgIsPayForFibreMatchesDecoder(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	pffMsg := blobfactory.NewMsgPayForFibre(t, secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey), "test")
	pffTx := newUnsignedTx(t, txConfig, pffMsg)
	sendMsg := newMsgSend()
	sendTx := newUnsignedTx(t, txConfig, sendMsg)
	field := func(num protowire.Number, v []byte) []byte {
		return protowire.AppendBytes(protowire.AppendTag(nil, num, protowire.BytesType), v)
	}
	var sendRaw, pffRaw txtypes.TxRaw
	require.NoError(t, sendRaw.Unmarshal(sendTx))
	require.NoError(t, pffRaw.Unmarshal(pffTx))
	noBody := sendRaw
	noBody.BodyBytes = nil
	sendRest, err := noBody.Marshal()
	require.NoError(t, err)
	pffValue, err := pffMsg.Marshal()
	require.NoError(t, err)
	sendValue, err := sendMsg.Marshal()
	require.NoError(t, err)
	sendURL := sdk.MsgTypeURL(&banktypes.MsgSend{})
	// withAny replaces the body of pffTx with one message whose Any holds both type URLs.
	withAny := func(first, last string, value []byte) []byte {
		anyBz := append(append(field(anyTypeURLField, []byte(first)), field(anyTypeURLField, []byte(last))...), field(2, value)...)
		raw := sendRaw
		raw.BodyBytes = field(txBodyMsgsField, anyBz)
		bz, err := raw.Marshal()
		require.NoError(t, err)
		return bz
	}

	tests := []struct {
		name    string
		rawTx   []byte
		wantPFF bool
	}{
		{"duplicated body_bytes, pay-for-fibre last", append(field(txRawBodyField, sendRaw.BodyBytes), pffTx...), true},
		{"duplicated body_bytes, MsgSend last", append(field(txRawBodyField, pffRaw.BodyBytes), sendTx...), false},
		{"duplicated body_bytes, empty last", slices.Concat(field(txRawBodyField, pffRaw.BodyBytes), field(txRawBodyField, nil), sendRest), false},
		{"duplicated type_url, pay-for-fibre last", withAny(sendURL, payForFibreTypeURL, pffValue), true},
		{"duplicated type_url, MsgSend last", withAny(payForFibreTypeURL, sendURL, sendValue), false},
		{"empty body", newUnsignedTx(t, txConfig), false},
		{"MsgSend first", newUnsignedTx(t, txConfig, newMsgSend(), pffMsg), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sdkTx, err := txConfig.TxDecoder()(tc.rawTx)
			require.NoError(t, err)
			decodedPFF := false
			if msgs := sdkTx.GetMsgs(); len(msgs) > 0 {
				_, decodedPFF = msgs[0].(*fibretypes.MsgPayForFibre)
			}
			require.Equal(t, tc.wantPFF, decodedPFF, "decoder")

			isPFF, ok := firstMsgIsPayForFibre(tc.rawTx)
			require.True(t, ok)
			require.Equal(t, tc.wantPFF, isPFF)
		})
	}
}
