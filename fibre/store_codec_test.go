package fibre

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"github.com/stretchr/testify/require"
)

func TestShardCodecRoundTrip(t *testing.T) {
	shard := &types.BlobShard{
		Root:         bytes.Repeat([]byte{0xab}, 32),
		Coefficients: bytes.Repeat([]byte{0xcd}, 64),
		Rows: []*types.BlobRow{
			{Index: 0, Data: bytes.Repeat([]byte{0x01}, 1024), Proof: [][]byte{[]byte("seg-a"), []byte("seg-b")}},
			{Index: 7, Data: bytes.Repeat([]byte{0x02}, 2048), Proof: nil},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, writeShardBinary(&buf, shard))

	got, err := readShardBinary(&buf)
	require.NoError(t, err)
	require.Equal(t, shard.Root, got.Root)
	require.Equal(t, shard.Coefficients, got.Coefficients)
	require.Len(t, got.Rows, len(shard.Rows))
	for i, r := range shard.Rows {
		require.Equal(t, r.Index, got.Rows[i].Index)
		require.Equal(t, r.Data, got.Rows[i].Data)
		require.Equal(t, len(r.Proof), len(got.Rows[i].Proof))
		for j, seg := range r.Proof {
			require.Equal(t, seg, got.Rows[i].Proof[j])
		}
	}
}

func TestShardCodecRejectsBomb(t *testing.T) {
	tests := []struct {
		name      string
		buildFile func() []byte
		wantSub   string
	}{
		{
			name: "numRows above cap",
			buildFile: func() []byte {
				var b []byte
				b = binary.BigEndian.AppendUint32(b, shardCodecVersion)
				b = binary.BigEndian.AppendUint32(b, 0) // root_len
				b = binary.BigEndian.AppendUint32(b, 0) // coeffs_len
				b = binary.BigEndian.AppendUint32(b, maxShardRows+1)
				return b
			},
			wantSub: "num rows",
		},
		{
			name: "numProof above cap",
			buildFile: func() []byte {
				var b []byte
				b = binary.BigEndian.AppendUint32(b, shardCodecVersion)
				b = binary.BigEndian.AppendUint32(b, 0) // root_len
				b = binary.BigEndian.AppendUint32(b, 0) // coeffs_len
				b = binary.BigEndian.AppendUint32(b, 1) // numRows = 1
				b = binary.BigEndian.AppendUint32(b, 0) // row index
				b = binary.BigEndian.AppendUint32(b, 0) // row data_len
				b = binary.BigEndian.AppendUint32(b, maxRowProofSegments+1)
				return b
			},
			wantSub: "num proof",
		},
		{
			name: "byte length above 1 GiB cap",
			buildFile: func() []byte {
				var b []byte
				b = binary.BigEndian.AppendUint32(b, shardCodecVersion)
				b = binary.BigEndian.AppendUint32(b, shardLengthLimit+1) // root_len
				return b
			},
			wantSub: "exceeds shard limit",
		},
		{
			name: "unknown version",
			buildFile: func() []byte {
				return binary.BigEndian.AppendUint32(nil, 99)
			},
			wantSub: "unsupported shard codec version",
		},
		{
			name: "truncated header",
			buildFile: func() []byte {
				return []byte{0x00, 0x00} // 2 bytes, want 4
			},
			wantSub: "reading version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readShardBinary(bytes.NewReader(tt.buildFile()))
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantSub)
		})
	}
}

// A file truncated partway through must error, not return a partial BlobShard.
func TestShardCodecTruncatedMidRow(t *testing.T) {
	shard := &types.BlobShard{
		Root: bytes.Repeat([]byte{0xab}, 32),
		Rows: []*types.BlobRow{
			{Index: 0, Data: bytes.Repeat([]byte{0x01}, 1024)},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, writeShardBinary(&buf, shard))

	full := buf.Bytes()
	for cut := 1; cut < len(full); cut += max(1, len(full)/16) {
		_, err := readShardBinary(bytes.NewReader(full[:cut]))
		require.Error(t, err, "cut at %d should fail", cut)
		require.True(t, errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF), "cut at %d: got %v", cut, err)
	}
}
