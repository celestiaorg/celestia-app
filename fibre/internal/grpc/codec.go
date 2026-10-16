package grpc

import (
	"fmt"
	"math"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cometbft/cometbft/libs/protoresource"
	"github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc/encoding"
	_ "google.golang.org/grpc/encoding/proto" // standard codec, used for non-gogoproto messages
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// codecName is the gRPC content-subtype clients select via
// grpc.CallContentSubtype.
const codecName = "fibre-proto"

// maxDownloadShardRequestSize bounds the wire size of a DownloadShardRequest.
// A valid request is 35 bytes; the slack leaves room for small future fields.
const maxDownloadShardRequestSize = 1024

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
	maxReceiveBytes  int
	downloadProfile  *protoresource.Profile
	uploadProfile    *protoresource.Profile
}

func init() {
	encoding.RegisterCodecV2(&pooledCodec{pool: mem.DefaultBufferPool()})
}

// NewServerCodec returns a codec that rejects upload requests with more than
// maxShardRows rows per shard or maxProofSegments per row before protobuf
// allocates for them. All limits must be positive: a zero limit would
// silently disable the check. Install it with [grpc.ForceServerCodecV2].
func NewServerCodec(maxShardRows, maxProofSegments, maxReceiveBytes int) encoding.CodecV2 {
	profile := shardProfile("celestia.fibre.v1.UploadShardRequest", maxShardRows, maxProofSegments, maxReceiveBytes)
	return &pooledCodec{pool: mem.DefaultBufferPool(), maxShardRows: maxShardRows, maxProofSegments: maxProofSegments, maxReceiveBytes: maxReceiveBytes, uploadProfile: profile}
}

// NewClientCodec bounds shard responses using the same schema policy as uploads.
func NewClientCodec(maxShardRows, maxProofSegments, maxReceiveBytes int) encoding.CodecV2 {
	return &pooledCodec{pool: mem.DefaultBufferPool(), maxReceiveBytes: maxReceiveBytes, downloadProfile: shardProfile("celestia.fibre.v1.DownloadShardResponse", maxShardRows, maxProofSegments, maxReceiveBytes)}
}

func shardProfile(name protoreflect.FullName, maxShardRows, maxProofSegments, maxReceiveBytes int) *protoresource.Profile {
	if maxShardRows <= 0 || maxProofSegments <= 0 || maxReceiveBytes <= 0 || maxReceiveBytes > math.MaxInt32 {
		panic("fibre-proto codec: limits must be positive and fit the gRPC byte limit")
	}
	units := uint64(maxReceiveBytes)*4 + 4096
	proofs, rows := uint64(maxProofSegments), uint64(maxShardRows)
	if proofs > (math.MaxUint64-8192)/128 || rows > (math.MaxUint64-units)/(8192+proofs*128) {
		panic("fibre-proto codec: resource limits overflow")
	}
	units += rows * (8192 + proofs*128)
	profile, err := protoresource.New(proto.HybridResolver, name, protoresource.Limits{
		Bytes: uint64(maxReceiveBytes), Fields: uint64(maxReceiveBytes), Messages: uint64(maxReceiveBytes), Elements: uint64(maxReceiveBytes), CopyBytes: uint64(maxReceiveBytes), Depth: 100, AllocationUnits: units,
	}, protoresource.FieldLimits{"celestia.fibre.v1.BlobShard.rows": rows, "celestia.fibre.v1.BlobRow.proof": rows * proofs})
	if err != nil {
		panic(fmt.Errorf("fibre-proto codec: compile resource profile: %w", err))
	}
	return profile
}

func (c *pooledCodec) Name() string { return codecName }

func (c *pooledCodec) Marshal(v any) (mem.BufferSlice, error) {
	if req, ok := v.(*types.UploadShardRequest); ok {
		return marshalUploadShardRequestScatter(req)
	}

	msg, ok := v.(sizedBufferMarshaler)
	if !ok {
		// The server forces this codec for every RPC on its listener, including
		// the gRPC health service whose messages are not gogoproto types.
		return encoding.GetCodecV2("proto").Marshal(v)
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
		return encoding.GetCodecV2("proto").Unmarshal(data, v)
	}
	if data.Len() == 0 {
		return msg.Unmarshal(nil)
	}
	// Reject oversized download requests before Materialize copies them.
	if _, ok := v.(*types.DownloadShardRequest); ok && data.Len() > maxDownloadShardRequestSize {
		return fmt.Errorf("fibre-proto codec: download request exceeds %d bytes", maxDownloadShardRequestSize)
	}
	if (c.uploadProfile != nil || c.downloadProfile != nil) && data.Len() > c.maxReceiveBytes {
		return protoresource.ErrBudget
	}
	buf := data.Materialize()
	// Check row and proof counts before the generated decoder allocates for them.
	if _, ok := v.(*types.UploadShardRequest); ok && c.maxShardRows > 0 {
		if err := c.uploadProfile.Check(buf); err != nil {
			return err
		}
		if err := c.validateUploadShard(buf); err != nil {
			return err
		}
	}
	if _, ok := v.(*types.DownloadShardResponse); ok && c.downloadProfile != nil {
		if err := c.downloadProfile.Check(buf); err != nil {
			return err
		}
	}
	return msg.Unmarshal(buf)
}
