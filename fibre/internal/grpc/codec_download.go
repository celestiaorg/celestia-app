package grpc

import (
	"sync"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/mem"
)

// DownloadCodec decodes one DownloadShard response into a recycled buffer that
// its rows alias. Create one per RPC, pass it with [grpc.ForceCodecV2], and call
// [DownloadCodec.Release] once no row of the response is referenced anymore.
// It bounds rows and proof segments per shard like [NewServerCodec].
type DownloadCodec struct {
	pooledCodec
	buf []byte
}

func NewDownloadCodec(maxShardRows, maxProofSegments int) *DownloadCodec {
	return &DownloadCodec{pooledCodec: *NewServerCodec(maxShardRows, maxProofSegments).(*pooledCodec)}
}

func (c *DownloadCodec) Unmarshal(data mem.BufferSlice, v any) error {
	resp, ok := v.(*types.DownloadShardResponse)
	// gRPC tracing formats messages after the RPC returns, so a recycled
	// buffer could be overwritten while still referenced.
	if !ok || data.Len() == 0 || grpc.EnableTracing {
		return c.pooledCodec.Unmarshal(data, v)
	}
	buf := downloadBuffers.get(data.Len())
	data.CopyTo(buf) // Overwrite every visible byte before parsing.
	aliased, err := c.unmarshalDownload(buf, resp)
	if aliased {
		c.buf = buf
		return nil
	}
	downloadBuffers.put(buf)
	return err
}

// Release recycles the response buffer. The decoded response must not be used
// afterwards. Safe to call more than once and before any response arrived.
func (c *DownloadCodec) Release() {
	if c.buf != nil {
		downloadBuffers.put(c.buf)
		c.buf = nil
	}
}

// downloadBuffers recycles response buffers through the GC-managed sync.Pool,
// which keeps their zeroing and collection off the download path.
var downloadBuffers bufferPool

type bufferPool struct {
	pool sync.Pool
}

// get returns a buffer of n bytes. A pooled buffer is reused only when it fits
// n without wasting more than half of it, so one blob shape's buffers do not
// stay pinned by a much smaller one.
func (p *bufferPool) get(n int) []byte {
	if buf, _ := p.pool.Get().(*[]byte); buf != nil && cap(*buf) >= n && cap(*buf) <= 2*n {
		return (*buf)[:n]
	}
	return make([]byte, n)
}

func (p *bufferPool) put(buf []byte) {
	p.pool.Put(&buf)
}
