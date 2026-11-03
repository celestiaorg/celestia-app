package grpc

import (
	"bytes"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"google.golang.org/grpc/mem"
)

// errUploadReleased is returned when an upload is marshaled after its owner
// released the storage its rows alias.
var errUploadReleased = errors.New("fibre-proto codec: upload rows already released")

// uploadOwners maps a tracked UploadShardRequest to the retain/release pair
// guarding the pooled storage its zero-copy row views alias.
var uploadOwners sync.Map // *types.UploadShardRequest -> owner

type owner struct {
	retain  func() bool
	release func()
}

// TrackUpload ties req's row views to an owner. Every marshal of req takes a
// reference with retain and drops it once gRPC has freed that marshal's views.
// An RPC can return (cancellation, early error status) while the transport
// still has queued data frames for it, so the rows must outlive the call, not
// just its return. Call the returned func once no further RPC will send req.
func TrackUpload(req *types.UploadShardRequest, retain func() bool, release func()) (untrack func()) {
	uploadOwners.Store(req, owner{retain: retain, release: release})
	return func() { uploadOwners.Delete(req) }
}

// sendRef is the mem.BufferPool backing one marshal's wrapped row views; it
// releases the owner's reference once the last view is freed.
type sendRef struct {
	views   atomic.Int64
	release func()
}

func (r *sendRef) Get(int) *[]byte { panic("fibre-proto codec: sendRef does not allocate") }

func (r *sendRef) Put(*[]byte) {
	if r.views.Add(-1) == 0 {
		r.release()
	}
}

// ownedViews wraps the pooled row views in bs (produced by
// marshalUploadShardRequestScatter for req) so their final Free releases one
// reference on req's owner. Views below the pooling threshold are copied, since
// gRPC would not ref-count them; framing and RLC views are GC-owned and left
// untouched. Untracked requests are returned unchanged.
func ownedViews(req *types.UploadShardRequest, bs mem.BufferSlice) (mem.BufferSlice, error) {
	v, ok := uploadOwners.Load(req)
	if !ok || req.Shard == nil {
		return bs, nil
	}
	o := v.(owner)
	if !o.retain() {
		bs.Free()
		return nil, errUploadReleased
	}

	pooled := make(map[*byte]bool, len(req.Shard.Rows))
	for _, row := range req.Shard.Rows {
		if row != nil && len(row.Data) > 0 {
			pooled[&row.Data[0]] = true
		}
	}

	ref := &sendRef{release: o.release}
	ref.views.Store(1) // held until every view is wrapped
	for i, b := range bs {
		s, ok := b.(*mem.SliceBuffer)
		if !ok || len(*s) == 0 || !pooled[&(*s)[0]] {
			continue
		}
		if data := []byte(*s); mem.IsBelowBufferPoolingThreshold(cap(data)) {
			bs[i] = mem.SliceBuffer(bytes.Clone(data))
		} else {
			ref.views.Add(1)
			bs[i] = mem.NewBuffer(&data, ref)
		}
	}
	ref.Put(nil)
	return bs, nil
}
