package app

import (
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/stretchr/testify/require"
)

func TestBlobTxIsCanonical(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	canonical := newBlobTx(t, txConfig)

	bTx, isBlob, err := blobtx.UnmarshalBlobTx(canonical)
	require.True(t, isBlob)
	require.NoError(t, err)
	require.True(t, blobTxIsCanonical(canonical, bTx), "a freshly marshaled blob tx is canonical")

	padded := appendUnknownProtoField(canonical, 4096)
	paddedTx, isBlob, err := blobtx.UnmarshalBlobTx(padded)
	require.True(t, isBlob)
	require.NoError(t, err, "unknown protobuf fields decode without error")
	require.False(t, blobTxIsCanonical(padded, paddedTx), "a padded blob tx is not canonical")
}

func TestSeparateTxsDropsNonCanonicalBlobTx(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	padded := appendUnknownProtoField(newBlobTx(t, txConfig), 4096)

	normalTxs, blobTxs, pffTxs := separateTxs(log.NewNopLogger(), txConfig, [][]byte{padded})
	require.Empty(t, normalTxs)
	require.Empty(t, pffTxs)
	require.Empty(t, blobTxs, "a non-canonically encoded blob tx must be dropped")
}

// appendUnknownProtoField appends a length-delimited unknown protobuf field
// (field number 100, wire type 2) whose value is padLen zero bytes.
// proto.Unmarshal accepts it, but MarshalBlobTx drops it, so the result decodes
// to the same blob tx while being a distinct, non-canonical encoding.
func appendUnknownProtoField(raw []byte, padLen int) []byte {
	tag := protoVarint(uint64(100)<<3 | 2)
	length := protoVarint(uint64(padLen))
	out := make([]byte, 0, len(raw)+len(tag)+len(length)+padLen)
	out = append(out, raw...)
	out = append(out, tag...)
	out = append(out, length...)
	out = append(out, make([]byte, padLen)...)
	return out
}

func protoVarint(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}
