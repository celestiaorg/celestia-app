package grpc

import (
	"math"
	"slices"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/protobuf/encoding/protowire"
)

// unmarshalUploadViews recognizes the field order emitted by the scatter marshaler.
// data must be GC-owned, and row/proof counts must already have been validated.
// Unsupported layouts leave dst unchanged for the generated decoder to handle.
func unmarshalUploadViews(data []byte, dst *types.UploadShardRequest) bool {
	if dst.Promise != nil || dst.Shard != nil {
		return false
	}
	var decoded types.UploadShardRequest
	wire := uploadWire(data)
	if wire.has(0x0a) {
		promise, ok := wire.bytes()
		if !ok {
			return false
		}
		decoded.Promise = &types.PaymentPromise{}
		if err := decoded.Promise.Unmarshal(promise); err != nil {
			return false
		}
	}
	if wire.has(0x12) {
		shard, ok := wire.bytes()
		if !ok {
			return false
		}
		decoded.Shard, ok = unmarshalShardViews(shard)
		if !ok {
			return false
		}
	}
	if len(wire) != 0 {
		return false
	}
	*dst = decoded
	return true
}

func unmarshalShardViews(data []byte) (*types.BlobShard, bool) {
	shard := &types.BlobShard{}
	wire := uploadWire(data)
	for wire.has(0x0a) {
		rowData, ok := wire.bytes()
		if !ok {
			return nil, false
		}
		row, ok := unmarshalRowViews(rowData)
		if !ok {
			return nil, false
		}
		shard.Rows = append(shard.Rows, row)
	}
	if wire.has(0x12) {
		var ok bool
		shard.Rlcs, ok = wire.bytes()
		if !ok || len(shard.Rlcs) == 0 {
			return nil, false
		}
	}
	return shard, len(wire) == 0
}

func unmarshalRowViews(data []byte) (*types.BlobRow, bool) {
	row := &types.BlobRow{}
	wire := uploadWire(data)
	if wire.has(0x08) {
		index, n := protowire.ConsumeVarint(wire[1:])
		if n < 0 || n != protowire.SizeVarint(index) || index == 0 || index > math.MaxUint32 {
			return nil, false
		}
		row.Index = uint32(index)
		wire = wire[n+1:]
	}
	if wire.has(0x12) {
		var ok bool
		row.Data, ok = wire.bytes()
		if !ok || len(row.Data) == 0 {
			return nil, false
		}
	}
	for wire.has(0x1a) {
		proof, ok := wire.bytes()
		if !ok {
			return nil, false
		}
		row.Proof = append(row.Proof, proof)
	}
	return row, len(wire) == 0
}

type uploadWire []byte

func (w uploadWire) has(tag byte) bool { return len(w) > 0 && w[0] == tag }

// bytes consumes a known one-byte tag and a canonical length-delimited field.
func (w *uploadWire) bytes() ([]byte, bool) {
	value, n := protowire.ConsumeBytes((*w)[1:])
	if n < 0 || n != protowire.SizeBytes(len(value)) {
		return nil, false
	}
	*w = (*w)[n+1:]
	// Appending to one field must not overwrite another field in the backing buffer.
	return slices.Clip(value), true
}
