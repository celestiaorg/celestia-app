package fibre

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
)

func TestShardCodecRoundTrip(t *testing.T) {
	shard := &types.BlobShard{
		Rlcs: bytes.Repeat([]byte{0xcd}, 64),
		Rows: []*types.BlobRow{
			{Index: 0, Data: bytes.Repeat([]byte{0x01}, 1024), Proof: [][]byte{[]byte("seg-a"), []byte("seg-b")}},
			{Index: 7, Data: bytes.Repeat([]byte{0x02}, 2048), Proof: nil},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, writeShardBinary(&buf, shard))

	got, err := readShardBinary(&buf)
	require.NoError(t, err)
	require.Equal(t, shard.Rlcs, got.Rlcs)
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
				b = binary.BigEndian.AppendUint32(b, 0) // rlcs_len
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
				b = binary.BigEndian.AppendUint32(b, 0) // rlcs_len
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
				b = binary.BigEndian.AppendUint32(b, shardLengthLimit+1) // rlcs_len
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

// FuzzShardCodecRoundTrip builds a structurally valid BlobShard from each
// fuzz seed, round-trips it through write/read, and asserts equality.
func FuzzShardCodecRoundTrip(f *testing.F) {
	f.Add([]byte{0x01, 0x02, 0x03, 0x04})
	f.Add(bytes.Repeat([]byte{0xff}, 128))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, seed []byte) {
		shard := shardFromSeed(seed)

		var buf bytes.Buffer
		require.NoError(t, writeShardBinary(&buf, shard))

		got, err := readShardBinary(&buf)
		require.NoError(t, err)
		require.Equal(t, shard.Rlcs, got.Rlcs)
		require.Len(t, got.Rows, len(shard.Rows))
		for i, r := range shard.Rows {
			require.Equal(t, r.Index, got.Rows[i].Index)
			require.Equal(t, r.Data, got.Rows[i].Data)
			require.Equal(t, len(r.Proof), len(got.Rows[i].Proof))
			for j, seg := range r.Proof {
				require.Equal(t, seg, got.Rows[i].Proof[j])
			}
		}
	})
}

// FuzzShardCodecReadNoPanic feeds arbitrary bytes to the reader. Length caps
// keep allocations bounded, so the only outcomes are a parse (rare) or an
// error — never a panic.
func FuzzShardCodecReadNoPanic(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x00, 0x00, 0x01})
	f.Add(bytes.Repeat([]byte{0xff}, 64))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = readShardBinary(bytes.NewReader(data))
	})
}

// shardFromSeed consumes seed deterministically to build a BlobShard with
// small bounded dimensions, so writeShardBinary always succeeds and a clean
// round-trip is possible regardless of seed contents.
func shardFromSeed(seed []byte) *types.BlobShard {
	r := &seedReader{seed: seed}
	shard := &types.BlobShard{
		Rlcs: r.take(int(r.byte() % 64)),
	}
	numRows := int(r.byte() % 8)
	shard.Rows = make([]*types.BlobRow, numRows)
	for i := range shard.Rows {
		row := &types.BlobRow{
			Index: binary.BigEndian.Uint32(r.take(4)),
			Data:  r.take(int(r.byte())),
		}
		numProof := int(r.byte() % 8)
		if numProof > 0 {
			row.Proof = make([][]byte, numProof)
			for j := range row.Proof {
				row.Proof[j] = r.take(int(r.byte() % 64))
			}
		}
		shard.Rows[i] = row
	}
	return shard
}

type seedReader struct {
	seed []byte
	pos  int
}

func (r *seedReader) byte() byte {
	if r.pos >= len(r.seed) {
		return 0
	}
	b := r.seed[r.pos]
	r.pos++
	return b
}

func (r *seedReader) take(n int) []byte {
	if n == 0 {
		return nil
	}
	out := make([]byte, n)
	for i := range out {
		if r.pos < len(r.seed) {
			out[i] = r.seed[r.pos]
			r.pos++
		}
	}
	return out
}

// A file truncated partway through must error, not return a partial BlobShard.
func TestShardCodecTruncatedMidRow(t *testing.T) {
	shard := &types.BlobShard{
		Rlcs: bytes.Repeat([]byte{0xab}, 32),
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
