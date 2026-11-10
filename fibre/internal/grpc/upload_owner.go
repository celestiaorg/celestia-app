package grpc

import (
	"errors"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"google.golang.org/grpc/mem"
)

// errUploadReleased is returned when an upload is marshaled after its owner
// released the storage its rows alias.
var errUploadReleased = errors.New("fibre-proto codec: upload rows already released")

// UploadCodec marshals UploadShard requests whose zero-copy row views alias
// storage guarded by retain/release. An RPC can return (cancellation, early
// error status) while the transport still has queued data frames for it, so
// the rows must outlive the call, not just its return: every marshaled row
// view takes a reference with retain and drops it once gRPC frees that view.
// Pass it with [grpc.ForceCodecV2] to every RPC sending such a request.
type UploadCodec struct {
	pooledCodec
	rows rowOwner
}

func NewUploadCodec(retain func() bool, release func()) *UploadCodec {
	return &UploadCodec{
		pooledCodec: pooledCodec{pool: mem.DefaultBufferPool()},
		rows:        rowOwner{retain: retain, release: release},
	}
}

func (c *UploadCodec) Marshal(v any) (mem.BufferSlice, error) {
	if req, ok := v.(*types.UploadShardRequest); ok {
		return marshalUploadShardRequestScatter(req, &c.rows)
	}
	return c.pooledCodec.Marshal(v)
}

// rowOwner is the mem.BufferPool backing ref-counted row views: freeing a view
// releases the reference retained for it.
type rowOwner struct {
	retain  func() bool
	release func()
}

func (o *rowOwner) Get(int) *[]byte { panic("fibre-proto codec: rowOwner does not allocate") }

func (o *rowOwner) Put(*[]byte) { o.release() }
