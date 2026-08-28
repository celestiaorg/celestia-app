package grpc

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
)

// Small limits keep these tests cheap. The server derives its limits from
// ProtocolParams.
const (
	testMaxRows   = 3
	testMaxProofs = 2
)

// makeUploadShard builds a request with the given rows and proofs per row. Data
// and RLCs exercise the counting decoders' skip paths.
func makeUploadShard(rows, proofsPerRow int) *types.UploadShardRequest {
	proof := make([][]byte, proofsPerRow)
	for i := range proof {
		proof[i] = make([]byte, 32)
	}
	blobRows := make([]*types.BlobRow, rows)
	for i := range blobRows {
		blobRows[i] = &types.BlobRow{Index: uint32(i), Data: []byte{0xff}, Proof: proof}
	}
	return &types.UploadShardRequest{Shard: &types.BlobShard{Rows: blobRows, Rlcs: make([]byte, 16)}}
}

func marshalUploadShard(t *testing.T, req *types.UploadShardRequest) []byte {
	t.Helper()
	buf, err := req.Marshal()
	require.NoError(t, err)
	return buf
}

// marshalShard marshals a request built by makeUploadShard.
func marshalShard(t *testing.T, rows, proofsPerRow int) []byte {
	t.Helper()
	return marshalUploadShard(t, makeUploadShard(rows, proofsPerRow))
}

// unmarshalUpload decodes buf as an UploadShardRequest through codec.
func unmarshalUpload(codec encoding.CodecV2, buf []byte) error {
	return codec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(buf)}, &types.UploadShardRequest{})
}

func TestValidateUploadShard(t *testing.T) {
	codec := &pooledCodec{maxShardRows: testMaxRows, maxProofSegments: testMaxProofs}

	tests := []struct {
		name         string
		rows         int
		proofsPerRow int
		wantErr      string
	}{
		{name: "empty request"},
		{name: "at row and proof limits", rows: testMaxRows, proofsPerRow: testMaxProofs},
		{name: "one row over limit", rows: testMaxRows + 1, proofsPerRow: 1, wantErr: "rows"},
		{name: "one proof over limit", rows: 1, proofsPerRow: testMaxProofs + 1, wantErr: "proof segments"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := codec.validateUploadShard(marshalShard(t, tc.rows, tc.proofsPerRow))
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}

	t.Run("only last row over proof limit", func(t *testing.T) {
		req := makeUploadShard(testMaxRows, 1)
		req.Shard.Rows[testMaxRows-1].Proof = make([][]byte, testMaxProofs+1)
		require.ErrorContains(t, codec.validateUploadShard(marshalUploadShard(t, req)), "proof segments")
	})

	t.Run("empty rows counted", func(t *testing.T) {
		// The attack shape: a fully empty BlobRow costs ~2 wire bytes.
		rows := make([]*types.BlobRow, testMaxRows+1)
		for i := range rows {
			rows[i] = &types.BlobRow{}
		}
		buf := marshalUploadShard(t, &types.UploadShardRequest{Shard: &types.BlobShard{Rows: rows}})
		require.ErrorContains(t, codec.validateUploadShard(buf), "rows")
	})

	t.Run("empty proof segments counted", func(t *testing.T) {
		row := &types.BlobRow{Proof: make([][]byte, testMaxProofs+1)}
		buf := marshalUploadShard(t, &types.UploadShardRequest{Shard: &types.BlobShard{Rows: []*types.BlobRow{row}}})
		require.ErrorContains(t, codec.validateUploadShard(buf), "proof segments")
	})

	t.Run("rows split across duplicate shard fields", func(t *testing.T) {
		// proto3 merges duplicate embedded-message fields, so concatenating two
		// marshalled requests yields one request whose rows accumulate across
		// both shard occurrences. The stub must count them the same way.
		doubled := bytes.Repeat(marshalShard(t, 2, 0), 2)

		var real types.UploadShardRequest
		require.NoError(t, real.Unmarshal(doubled))
		require.Len(t, real.Shard.Rows, 4)

		require.ErrorContains(t, codec.validateUploadShard(doubled), "rows")
	})

	t.Run("unknown trailing field skipped", func(t *testing.T) {
		buf := marshalShard(t, 1, 1)
		buf = protowire.AppendTag(buf, 7, protowire.VarintType)
		buf = protowire.AppendVarint(buf, 42)
		require.NoError(t, codec.validateUploadShard(buf))
	})

	t.Run("malformed bytes rejected", func(t *testing.T) {
		truncated := marshalShard(t, 2, 2)
		for _, bad := range [][]byte{
			truncated[:len(truncated)-1], // truncated payload
			{0x80},                       // unterminated tag varint
			protowire.AppendTag(nil, 7, protowire.VarintType), // tag without value
		} {
			require.Error(t, codec.validateUploadShard(bad))
		}
	})
}

// TestCodecUnmarshalLimits checks server limits and confirms the client codec
// remains unrestricted.
func TestCodecUnmarshalLimits(t *testing.T) {
	codec := NewServerCodec(testMaxRows, testMaxProofs)

	t.Run("rejects too many rows", func(t *testing.T) {
		require.ErrorContains(t, unmarshalUpload(codec, marshalShard(t, testMaxRows+1, 0)), "rows")
	})

	t.Run("rejects too many proofs", func(t *testing.T) {
		require.ErrorContains(t, unmarshalUpload(codec, marshalShard(t, 1, testMaxProofs+1)), "proof segments")
	})

	t.Run("round-trips a valid request", func(t *testing.T) {
		wire, err := codec.Marshal(makeUploadShard(testMaxRows, testMaxProofs))
		require.NoError(t, err)

		var got types.UploadShardRequest
		require.NoError(t, codec.Unmarshal(wire, &got))
		require.Len(t, got.Shard.Rows, testMaxRows)
		require.Len(t, got.Shard.Rows[0].Proof, testMaxProofs)
	})

	t.Run("client codec does not bound", func(t *testing.T) {
		clientCodec := &pooledCodec{pool: mem.DefaultBufferPool()}
		require.NoError(t, unmarshalUpload(clientCodec, marshalShard(t, testMaxRows+1, 0)))
	})

	t.Run("panics on non-positive limits", func(t *testing.T) {
		require.Panics(t, func() { NewServerCodec(0, testMaxProofs) })
		require.Panics(t, func() { NewServerCodec(testMaxRows, 0) })
	})
}

// TestCountingMessagesMatchRealDecode checks that the counting messages still
// use the same field numbers as the real request.
func TestCountingMessagesMatchRealDecode(t *testing.T) {
	rng := rand.New(rand.NewSource(7))

	for i := range 100 {
		req := makeUploadShard(rng.Intn(8), 0)
		for _, row := range req.Shard.Rows {
			row.Proof = make([][]byte, rng.Intn(5))
			for k := range row.Proof {
				row.Proof[k] = make([]byte, 32)
			}
		}
		if i%2 == 0 {
			req.Promise = &types.PaymentPromise{ChainId: "test", Height: int64(i)}
		}
		buf := marshalUploadShard(t, req)

		var rowCount types.RowCountUploadShard
		require.NoError(t, rowCount.Unmarshal(buf))
		require.Len(t, rowCount.Shard.Rows, len(req.Shard.Rows))

		var proofCount types.ProofCountUploadShard
		require.NoError(t, proofCount.Unmarshal(buf))
		require.Len(t, proofCount.Shard.Rows, len(req.Shard.Rows))
		for j, row := range req.Shard.Rows {
			require.Len(t, proofCount.Shard.Rows[j].Proof, len(row.Proof))
		}
	}
}

// requireCountingAllocsO1 asserts unmarshal decodes req in O(1) allocations
// rather than per repeated-field entry.
func requireCountingAllocsO1(t *testing.T, req *types.UploadShardRequest, unmarshal func([]byte) error) {
	t.Helper()
	buf := marshalUploadShard(t, req)
	allocs := testing.AllocsPerRun(10, func() {
		if err := unmarshal(buf); err != nil {
			t.Fatal(err)
		}
	})
	require.Less(t, allocs, 10.0, "counting must allocate O(1), not O(entries)")
}

// TestCountingAllocations checks that counting rows and proof segments does not
// allocate per entry.
func TestCountingAllocations(t *testing.T) {
	t.Run("rows", func(t *testing.T) {
		requireCountingAllocsO1(t, makeUploadShard(100_000, 0), func(buf []byte) error {
			var stub types.RowCountUploadShard
			return stub.Unmarshal(buf)
		})
	})

	t.Run("proofs", func(t *testing.T) {
		req := &types.UploadShardRequest{Shard: &types.BlobShard{
			Rows: []*types.BlobRow{{Proof: make([][]byte, 100_000)}},
		}}
		requireCountingAllocsO1(t, req, func(buf []byte) error {
			var stub types.ProofCountUploadShard
			return stub.Unmarshal(buf)
		})
	})
}
