package testnode

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/celestiaorg/celestia-app/v12/app"
	"github.com/celestiaorg/celestia-app/v12/app/encoding"
	"github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestReverseSlice(t *testing.T) {
	tests := []struct {
		input    any
		expected any
	}{
		{[]int{1, 2, 3, 4, 5}, []int{5, 4, 3, 2, 1}},
		{[]string{"a", "b", "c", "d"}, []string{"d", "c", "b", "a"}},
		{[]int{1, 2}, []int{2, 1}},
		{[]int{1}, []int{1}},
		{[]string{}, []string{}},
	}

	for _, tt := range tests {
		switch v := tt.input.(type) {
		case []int:
			// reverseSlice modifies the input slice, so we need to make a copy
			original := make([]int, len(tt.input.([]int)))
			copy(original, tt.input.([]int))
			reverseSlice(v)
			require.True(t, reflect.DeepEqual(v, tt.expected), "reverseSlice(%v) = %v, want %v", original, tt.input, tt.expected)
		case []string:
			// reverseSlice modifies the input slice, so we need to make a copy
			original := make([]string, len(tt.input.([]string)))
			copy(original, tt.input.([]string))
			reverseSlice(v)
			require.True(t, reflect.DeepEqual(v, tt.expected), "reverseSlice(%v) = %v, want %v", original, tt.input, tt.expected)
		}
	}
}

func TestReadBlockchainHeaders(t *testing.T) {
	cfg := DefaultConfig()
	cctx, rpcAddr, _ := NewNetwork(t, cfg)
	// wait for 30 blocks to be produced
	err := cctx.WaitForBlocks(30)
	require.NoError(t, err)

	// fetch headers
	headers, err := ReadBlockchainHeaders(context.Background(), rpcAddr)
	require.NoError(t, err)
	// we should have at least 30 headers
	require.True(t, len(headers) >= 30)

	// check that the headers are in ascending order, starting from 1
	i := int64(1)
	for _, header := range headers {
		got := header.Header.Height
		require.Equal(t, i, got,
			"expected height %d, got %d", i, got)
		i++
	}
}

// TestDecodeBlockDataReadsNonCanonicalTx checks that DecodeBlockData reads a
// committed tx that only the strict decoder rejects. Blocks committed before
// the strict rule can hold such txs.
func TestDecodeBlockDataReadsNonCanonicalTx(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	builder := enc.TxConfig.NewTxBuilder()
	from, to := sdk.AccAddress(bytes.Repeat([]byte{1}, 20)), sdk.AccAddress(bytes.Repeat([]byte{2}, 20))
	require.NoError(t, builder.SetMsgs(banktypes.NewMsgSend(from, to, sdk.NewCoins(sdk.NewInt64Coin("utia", 1)))))
	canonical, err := enc.TxConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)

	// Add a non-critical unknown field to the body. The default decoder
	// accepts it and the strict decoder rejects it.
	var raw txtypes.TxRaw
	require.NoError(t, raw.Unmarshal(canonical))
	raw.BodyBytes = protowire.AppendBytes(protowire.AppendTag(raw.BodyBytes, 1025, protowire.BytesType), []byte("x"))
	nonCanonical, err := raw.Marshal()
	require.NoError(t, err)
	_, err = enc.TxConfig.TxDecoder()(nonCanonical)
	require.Error(t, err)

	txs, err := DecodeBlockData(types.Data{Txs: types.Txs{nonCanonical}})
	require.NoError(t, err)
	require.Len(t, txs, 1)
}
