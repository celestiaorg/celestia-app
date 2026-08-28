package grpc

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
)

// codecName is the gRPC content-subtype clients select via
// grpc.CallContentSubtype.
const codecName = "fibre-proto"

type sizedBufferMarshaler interface {
	Size() int
	MarshalToSizedBuffer([]byte) (int, error)
}

type protoUnmarshaler interface {
	Unmarshal([]byte) error
}

// pooledCodec wraps gogoproto's MarshalToSizedBuffer + Unmarshal with
// per-RPC buffer reuse from gRPC's mem.BufferPool. For UploadShardRequest
// the scatter path emits row payloads zero-copy; every other message goes
// through the pooled contiguous path.
type pooledCodec struct {
	pool mem.BufferPool

	// These limits are zero for clients, which do not decode upload requests.
	// Servers set them with NewServerCodec.
	maxShardRows     int
	maxProofSegments int
}

func init() {
	encoding.RegisterCodecV2(&pooledCodec{pool: mem.DefaultBufferPool()})
}

// NewServerCodec returns a codec that rejects upload requests with more than
// maxShardRows rows per shard or maxProofSegments per row before protobuf
// allocates for them. Both limits must be positive: a zero limit would
// silently disable the check. Install it with [grpc.ForceServerCodecV2].
func NewServerCodec(maxShardRows, maxProofSegments int) encoding.CodecV2 {
	if maxShardRows <= 0 || maxProofSegments <= 0 {
		panic(fmt.Sprintf("fibre-proto codec: limits must be positive, got maxShardRows=%d maxProofSegments=%d", maxShardRows, maxProofSegments))
	}
	return &pooledCodec{
		pool:             mem.DefaultBufferPool(),
		maxShardRows:     maxShardRows,
		maxProofSegments: maxProofSegments,
	}
}

func (c *pooledCodec) Name() string { return codecName }

func (c *pooledCodec) Marshal(v any) (mem.BufferSlice, error) {
	if req, ok := v.(*types.UploadShardRequest); ok {
		return marshalUploadShardRequestScatter(req)
	}

	msg, ok := v.(sizedBufferMarshaler)
	if !ok {
		return nil, fmt.Errorf("fibre-proto codec: %T does not implement sizedBufferMarshaler", v)
	}

	size := msg.Size()
	if size == 0 {
		return mem.BufferSlice{}, nil
	}

	bufPtr := c.pool.Get(size)
	buf := (*bufPtr)[:size]

	// Size() is exact for gogoproto, so n == size; no reslice needed.
	if _, err := msg.MarshalToSizedBuffer(buf); err != nil {
		c.pool.Put(bufPtr)
		return nil, err
	}
	*bufPtr = buf
	return mem.BufferSlice{mem.NewBuffer(bufPtr, c.pool)}, nil
}

func (c *pooledCodec) Unmarshal(data mem.BufferSlice, v any) error {
	msg, ok := v.(protoUnmarshaler)
	if !ok {
		return fmt.Errorf("fibre-proto codec: %T does not implement protoUnmarshaler", v)
	}
	if data.Len() == 0 {
		return msg.Unmarshal(nil)
	}
	buf := data.Materialize()
	// Check row and proof counts before the generated decoder allocates for them.
	if _, ok := v.(*types.UploadShardRequest); ok && c.maxShardRows > 0 {
		if err := c.validateUploadShard(buf); err != nil {
			return err
		}
	}
	return msg.Unmarshal(buf)
}
