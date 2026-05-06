package grpc

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
)

// codecName is the gRPC content-subtype under which this codec is registered.
// Clients select it via grpc.CallContentSubtype; servers pick it up
// automatically when an inbound request advertises this subtype.
const codecName = "fibre-proto"

// sizedMarshaler is what every gogoproto-generated message implements. We
// rely on it to avoid the default proto.Marshal allocation of a fresh slice
// per RPC: instead we Size() up front, fetch a pooled buffer, and
// MarshalToSizedBuffer into it. The pool's Free hook (via mem.NewBuffer)
// returns the buffer to the pool after gRPC is done with it on the wire.
type sizedMarshaler interface {
	Size() int
	MarshalToSizedBuffer([]byte) (int, error)
}

// sizedUnmarshaler is the matching receive-side interface from gogoproto.
type sizedUnmarshaler interface {
	Unmarshal([]byte) error
}

// pooledCodec is a [encoding.CodecV2] implementation that wraps gogoproto's
// MarshalToSizedBuffer + Unmarshal with explicit buffer reuse via gRPC's
// mem.BufferPool.
//
// On the encoder hot path the default codec was the dominant CPU cost — see
// the Pyroscope profile that showed memmove + memclr at ≈ 39% of encoder
// CPU during c=20 single-encoder runs. proto.Marshal allocates a fresh
// 28 MiB slice for every shard send; with 9 fan-out targets and 20 in-flight
// blobs the encoder churns ~5 GiB of fresh allocs per upload. Pooling those
// buffers cuts the memclr (zero-fill on alloc) and memmove (no oversize
// realloc growth) costs sharply.
type pooledCodec struct {
	pool mem.BufferPool
}

func init() {
	// Register at init so the codec is available as soon as either
	// side imports this package.
	encoding.RegisterCodecV2(&pooledCodec{pool: mem.DefaultBufferPool()})
}

// Name implements [encoding.CodecV2].
func (c *pooledCodec) Name() string { return codecName }

// Marshal implements [encoding.CodecV2]. v must implement [sizedMarshaler]
// (every gogoproto message does).
//
// For [types.UploadShardRequest] the scatter-gather path in
// [marshalUploadShardRequestScatter] is taken: row payload bytes are
// emitted as zero-copy [mem.SliceBuffer] views over the caller's slab
// buffers, eliminating the ~28 MiB memmove that the contiguous-marshal
// path would do. For every other message (PaymentPromise responses,
// download requests, etc.) the contiguous pooled-buffer path below runs.
func (c *pooledCodec) Marshal(v any) (mem.BufferSlice, error) {
	if req, ok := v.(*types.UploadShardRequest); ok {
		return marshalUploadShardRequestScatter(req)
	}

	msg, ok := v.(sizedMarshaler)
	if !ok {
		return nil, fmt.Errorf("fibre-proto codec: %T does not implement sizedMarshaler", v)
	}

	size := msg.Size()
	if size == 0 {
		return nil, nil
	}

	// mem.BufferPool.Get returns *[]byte with cap >= size; the underlying
	// slice may have stale data but MarshalToSizedBuffer writes the full
	// `size` bytes so we don't need to zero it.
	bufPtr := c.pool.Get(size)
	buf := (*bufPtr)[:size]

	n, err := msg.MarshalToSizedBuffer(buf)
	if err != nil {
		c.pool.Put(bufPtr)
		return nil, err
	}
	// MarshalToSizedBuffer writes from the *end* of the buffer backward
	// (gogoproto convention) — the returned n is the length of the
	// written region; the start is at size-n.
	*bufPtr = buf[size-n : size]
	return mem.BufferSlice{mem.NewBuffer(bufPtr, c.pool)}, nil
}

// Unmarshal implements [encoding.CodecV2]. Materialize() copies the wire
// bytes into a single contiguous slice so gogoproto's Unmarshal can decode
// in one pass; the original [mem.BufferSlice] is freed by gRPC after this
// call returns.
func (c *pooledCodec) Unmarshal(data mem.BufferSlice, v any) error {
	msg, ok := v.(sizedUnmarshaler)
	if !ok {
		return fmt.Errorf("fibre-proto codec: %T does not implement sizedUnmarshaler", v)
	}
	if data.Len() == 0 {
		return msg.Unmarshal(nil)
	}
	// Materialize copies once; further parsing is in-place against the
	// resulting []byte. For the 28 MiB upload-shard request this is one
	// allocation per inbound RPC — same as the default proto codec but
	// with the upside that the SEND path is now pooled.
	buf := data.Materialize()
	return msg.Unmarshal(buf)
}
