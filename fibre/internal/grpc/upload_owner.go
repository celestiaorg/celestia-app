package grpc

import (
	"errors"
	"runtime"
	"sync/atomic"

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

// rowOwner retains and releases the storage that upload row views alias.
type rowOwner struct {
	retain  func() bool
	release func()
}

// wrap returns a ref-counted view of data holding one owner reference, or
// false when the owner is already released.
//
// The reference is dropped when gRPC frees the view. That happens only while
// the connection's writer runs: when the connection dies with frames still
// queued (grpc-go v1.84 controlbuf.finish drains only its control list, not
// the data frames queued on established streams), nothing frees them. A GC
// cleanup on the view's backing pointer drops the reference in that case. The
// pointer stays reachable from every gRPC reader of the view, so the cleanup
// cannot run while the bytes are still being sent.
func (o *rowOwner) wrap(data []byte) (mem.Buffer, bool) {
	if !o.retain() {
		return nil, false
	}
	ref := &rowRef{release: o.release}
	backing := new([]byte)
	*backing = data
	runtime.AddCleanup(backing, (*rowRef).drop, ref)
	return mem.NewBuffer(backing, ref), true
}

// rowRef is the mem.BufferPool backing one row view: freeing the view drops
// the owner reference retained for it, exactly once.
type rowRef struct {
	released atomic.Bool
	release  func()
}

func (r *rowRef) Get(int) *[]byte { panic("fibre-proto codec: rowRef does not allocate") }

func (r *rowRef) Put(*[]byte) { r.drop() }

func (r *rowRef) drop() {
	if r.released.CompareAndSwap(false, true) {
		r.release()
	}
}
