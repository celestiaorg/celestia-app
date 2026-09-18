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

func TestSeparateTxsDropsNonCanonicalBlobTx(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	padded := appendUnknownProtoField(newBlobTx(t, txConfig), 4096)

	normalTxs, blobTxs, rawBlobTxs, pffTxs := separateTxs(log.NewNopLogger(), txConfig, [][]byte{padded})
	require.Empty(t, normalTxs)
	require.Empty(t, pffTxs)
	require.Empty(t, blobTxs, "a non-canonically encoded blob tx must be dropped")
	require.Empty(t, rawBlobTxs, "a non-canonically encoded blob tx must be dropped")
}

// TestBlobTxCanonicalEncodingGolden pins the canonical encoding of a fixed blob
// tx so a wire-format change (e.g. a go-square or protobuf bump), which would be
// consensus breaking, fails this test.
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
	require.Equal(t, []byte("tx"), bTx.Tx)
	require.Len(t, bTx.Blobs, 1)
	require.Equal(t, blob, bTx.Blobs[0])
}

// appendUnknownProtoField appends an unknown protobuf field (field 100, wire
// type 2) of padLen zero bytes: proto.Unmarshal accepts it but MarshalBlobTx
// drops it, yielding a distinct, non-canonical encoding of the same blob tx.
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
