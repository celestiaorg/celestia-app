package fibre

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"
	"testing/iotest"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
)

// TestShardReader checks encoded bytes, size and seeking across all byte positions, including past EOF.
func TestShardReader(t *testing.T) {
	for name, shard := range map[string]*types.BlobShard{
		"empty":        {},
		"empty slices": {Rows: []*types.BlobRow{{Proof: [][]byte{nil, {}}}, {}}},
		"rows and proofs": {
			Rlcs: []byte("rlcs"),
			Rows: []*types.BlobRow{
				{Index: 4, Data: []byte("first row"), Proof: [][]byte{[]byte("proof"), nil, []byte("segment")}},
				{Index: 7, Data: []byte("second row")},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := newShardReader(shard)
			require.NoError(t, err)
			var encoded bytes.Buffer
			require.NoError(t, writeShardBinary(&encoded, shard))
			require.Equal(t, int64(encoded.Len()), r.size)
			require.NoError(t, iotest.TestReader(r, encoded.Bytes()))

			for pos := 0; pos <= encoded.Len()+1; pos++ {
				for _, whence := range []int{io.SeekStart, io.SeekCurrent, io.SeekEnd} {
					_, err := r.Seek(2, io.SeekStart)
					require.NoError(t, err)
					offset := int64(pos)
					switch whence {
					case io.SeekCurrent:
						offset -= 2
					case io.SeekEnd:
						offset -= r.size
					}
					got, err := r.Seek(offset, whence)
					require.NoError(t, err)
					require.Equal(t, int64(pos), got)
					data, err := io.ReadAll(r)
					require.NoError(t, err)
					require.Equal(t, encoded.Bytes()[min(pos, encoded.Len()):], data)
				}
			}
		})
	}
}

// TestShardReaderInvalidSeek checks that invalid seeks return errors without changing the reader position.
func TestShardReaderInvalidSeek(t *testing.T) {
	r, err := newShardReader(&types.BlobShard{})
	require.NoError(t, err)
	for _, seek := range []struct {
		offset int64
		whence int
	}{
		{-1, io.SeekStart},
		{-3, io.SeekCurrent},
		{-r.size - 1, io.SeekEnd},
		{0, 3},
		{math.MaxInt64, io.SeekCurrent},
		{math.MaxInt64, io.SeekEnd},
		{math.MinInt64, io.SeekCurrent},
	} {
		_, err := r.Seek(2, io.SeekStart)
		require.NoError(t, err)
		_, err = r.Seek(seek.offset, seek.whence)
		require.Error(t, err)
		pos, err := r.Seek(0, io.SeekCurrent)
		require.NoError(t, err)
		require.Equal(t, int64(2), pos)
	}
}

// TestShardReaderReusesPayload checks that the reader uses the original RLC, row data and proof slices.
func TestShardReaderReusesPayload(t *testing.T) {
	shard := &types.BlobShard{
		Rlcs: []byte("rlcs"),
		Rows: []*types.BlobRow{{Data: []byte("data"), Proof: [][]byte{[]byte("proof")}}},
	}
	r, err := newShardReader(shard)
	require.NoError(t, err)
	// Changes before reading must be visible through the original payload slices.
	shard.Rlcs[0]++
	shard.Rows[0].Data[0]++
	shard.Rows[0].Proof[0][0]++
	var encoded bytes.Buffer
	require.NoError(t, writeShardBinary(&encoded, shard))
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, encoded.Bytes(), data)
}

// TestShardReaderRejectsNil checks that nil shards and rows return errors.
func TestShardReaderRejectsNil(t *testing.T) {
	_, err := newShardReader(nil)
	require.ErrorContains(t, err, "nil shard")
	_, err = newShardReader(&types.BlobShard{Rows: []*types.BlobRow{nil}})
	require.ErrorContains(t, err, "nil row")
}

// FuzzShardReader compares mixed reads and seeks with bytes.Reader, including errors and EOF.
func FuzzShardReader(f *testing.F) {
	f.Add([]byte{}, []byte{0, 32, 0, 1, 0, 0, 0, 32, 0, 3, 0, 0, 0, 1, 0, 2, 255, 255, 4, 0, 0})
	f.Add(bytes.Repeat([]byte{0xff}, 128), bytes.Repeat([]byte{0}, 30))
	f.Add([]byte{0, 2}, bytes.Repeat([]byte{0xff}, 30))

	f.Fuzz(func(t *testing.T, seed, operations []byte) {
		shard := shardFromSeed(seed)
		var encoded bytes.Buffer
		require.NoError(t, writeShardBinary(&encoded, shard))
		want := bytes.NewReader(encoded.Bytes())
		got, err := newShardReader(shard)
		require.NoError(t, err)
		var gotBuf, wantBuf [1024]byte

		// Limit each input to 256 operations. Each operation uses one tag and a two-byte argument.
		operations = operations[:min(len(operations), 256*3)]
		for len(operations) >= 3 {
			op := operations[0] % 5
			arg := binary.LittleEndian.Uint16(operations[1:3])
			operations = operations[3:]
			if op == 0 {
				size := int(arg) % (len(gotBuf) + 1)
				wantN, wantErr := want.Read(wantBuf[:size])
				gotN, gotErr := got.Read(gotBuf[:size])
				require.Equal(t, wantErr, gotErr)
				require.Equal(t, wantN, gotN)
				require.Equal(t, wantBuf[:wantN], gotBuf[:gotN])
			} else {
				offset, whence := int64(int16(arg)), int(op)-1
				wantPos, wantErr := want.Seek(offset, whence)
				gotPos, gotErr := got.Seek(offset, whence)
				require.Equal(t, wantErr == nil, gotErr == nil)
				require.Equal(t, wantPos, gotPos)
			}
		}

		remaining, err := io.ReadAll(want)
		require.NoError(t, err)
		data, err := io.ReadAll(got)
		require.NoError(t, err)
		require.Equal(t, remaining, data)

		_, err = got.Seek(0, io.SeekStart)
		require.NoError(t, err)
		data, err = io.ReadAll(got)
		require.NoError(t, err)
		require.Equal(t, encoded.Bytes(), data)
	})
}
