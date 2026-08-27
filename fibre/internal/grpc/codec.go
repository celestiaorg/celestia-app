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
}

func init() {
	encoding.RegisterCodecV2(&pooledCodec{pool: mem.DefaultBufferPool()})
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
	return msg.Unmarshal(data.Materialize())
}
