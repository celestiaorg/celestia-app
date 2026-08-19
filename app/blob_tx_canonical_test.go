package app

import (
	"bytes"
	"encoding/hex"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/go-square/v4/share"
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

	// Repeat the singular type_id field (field 3, wire type 2, value "BLOB").
	// proto.Unmarshal keeps the last value, so it decodes to the same blob tx,
	// but the bytes are a distinct, non-canonical encoding.
	repeated := append(append([]byte{}, canonical...), 0x1a, 0x04, 'B', 'L', 'O', 'B')
	repeatedTx, isBlob, err := blobtx.UnmarshalBlobTx(repeated)
	require.True(t, isBlob)
	require.NoError(t, err, "repeated singular protobuf fields decode without error")
	require.False(t, blobTxIsCanonical(repeated, repeatedTx), "a blob tx with a repeated singular field is not canonical")
}

func TestSeparateTxsDropsNonCanonicalBlobTx(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	padded := appendUnknownProtoField(newBlobTx(t, txConfig), 4096)

	normalTxs, blobTxs, rawBlobTxs, pffTxs := separateTxs(log.NewNopLogger(), txConfig, [][]byte{padded})
	require.Empty(t, normalTxs)
	require.Empty(t, pffTxs)
	require.Empty(t, blobTxs, "a non-canonically encoded blob tx must be dropped")
	require.Empty(t, rawBlobTxs, "a non-canonically encoded blob tx must be dropped")
}

// TestBlobTxCanonicalEncodingGolden pins the canonical encoding of a fixed
// blob tx to a golden value. The canonical predicate compares raw bytes
// against MarshalBlobTx output, so any change to the wire format produced by
// go-square or the protobuf library (e.g. after a dependency bump) is
// consensus breaking and must fail this test.
func TestBlobTxCanonicalEncodingGolden(t *testing.T) {
	namespace := share.MustNewV0Namespace(bytes.Repeat([]byte{0x01}, share.NamespaceVersionZeroIDSize))
	blob, err := share.NewBlob(namespace, []byte("data"), share.ShareVersionZero, nil)
	require.NoError(t, err)

	raw, err := blobtx.MarshalBlobTx([]byte("tx"), blob)
	require.NoError(t, err)

	const golden = "0a02747812240a1c000000000000000000000000000000000000010101010101010101011204646174611a04424c4f42"
	require.Equal(t, golden, hex.EncodeToString(raw))

	bTx, isBlob, err := blobtx.UnmarshalBlobTx(raw)
	require.True(t, isBlob)
	require.NoError(t, err)
	require.True(t, blobTxIsCanonical(raw, bTx))
	require.Equal(t, []byte("tx"), bTx.Tx)
	require.Len(t, bTx.Blobs, 1)
	require.Equal(t, blob, bTx.Blobs[0])
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
