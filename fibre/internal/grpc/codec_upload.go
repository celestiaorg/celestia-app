package grpc

import (
	"fmt"
	"io"

	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"google.golang.org/grpc/mem"
)

// Protobuf field tags (field<<3 | wireType) for UploadShardRequest. Both fields
// are length-delimited (wireType 2).
const (
	tagReqPromise = 1<<3 | 2 // UploadShardRequest.promise
	tagReqShard   = 2<<3 | 2 // UploadShardRequest.shard
)

// decodeUploadShardRequest stream-decodes an UploadShardRequest into req,
// copying each shard row payload exactly once from the fragmented recv buffers
// into a single arena that BlobShard fields alias. It reuses the same arena /
// wireReader / decodeShard machinery as the download decoder.
//
// Lifetime asymmetry vs the download client: the client owns its reply object
// and recycles the arena via DownloadReply.Free, but the server receives a
// gRPC-owned *UploadShardRequest with no post-handler free hook, so the arena
// is GC-managed rather than pool-recycled. This still removes the stock path's
// per-row allocations and one full payload copy (Materialize, then gogoproto
// copies every bytes field out again). Aliasing the rows is safe because the
// UploadShard handler consumes the shard synchronously (verify + store write)
// and does not retain it past return.
func decodeUploadShardRequest(data mem.BufferSlice, req *types.UploadShardRequest) error {
	if data.Len() == 0 {
		return nil
	}

	arena := &arena{buf: make([]byte, data.Len())}
	r := data.Reader()
	defer r.Close()
	w := &wireReader{r}

	for {
		tag, _, err := w.uvarint()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		ln, _, err := w.uvarint()
		if err != nil {
			return err
		}
		switch tag {
		case tagReqPromise:
			// Promise is small; copy into the arena and gogoproto-decode it
			// (it owns its own field allocations after that).
			buf := arena.next(int(ln))
			if err := w.fill(buf); err != nil {
				return err
			}
			req.Promise = &types.PaymentPromise{}
			if err := req.Promise.Unmarshal(buf); err != nil {
				return err
			}
		case tagReqShard:
			req.Shard = &types.BlobShard{}
			if err := decodeShard(w, arena, req.Shard, int(ln)); err != nil {
				return err
			}
		default:
			return fmt.Errorf("fibre-proto: unexpected upload request tag %#x", tag)
		}
	}
}
