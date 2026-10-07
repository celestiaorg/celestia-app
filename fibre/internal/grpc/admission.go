package grpc

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
)

// Admission accounts for RPC working memory, including queued responses.
type Admission struct {
	mu                                    sync.Mutex
	total, downloadLimit, used, downloads int64
	maxMessage                            int
	codec                                 *pooledCodec
	rejected                              metric.Int64Counter
}

// NewAdmission reserves uploadReserve bytes exclusively for uploads. Zero total disables admission.
func NewAdmission(total, uploadReserve int64, maxMessage, maxRows, maxProofs int) *Admission {
	return &Admission{
		total: total, downloadLimit: total - uploadReserve, maxMessage: maxMessage,
		codec: NewServerCodec(maxRows, maxProofs).(*pooledCodec),
	}
}

// RegisterMetrics exposes reserved bytes and rejected reservations by direction.
func (a *Admission) RegisterMetrics(m metric.Meter) error {
	var err error
	_, err = m.Int64ObservableGauge("fibre.server.rpc.reserved_bytes", metric.WithUnit("By"),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			a.mu.Lock()
			used, downloads := a.used, a.downloads
			a.mu.Unlock()
			observer.Observe(downloads, metric.WithAttributes(attribute.Bool("download", true)))
			observer.Observe(used-downloads, metric.WithAttributes(attribute.Bool("download", false)))
			return nil
		}))
	if err != nil {
		return err
	}
	a.rejected, err = m.Int64Counter("fibre.server.rpc.memory_rejected")
	return err
}

type leaseKey struct{}

type memoryLease struct {
	budget   *Admission
	download bool
	bytes    int64
	refs     atomic.Int32
}

// ReserveMemory charges an allocation to the current RPC before it is made.
// Calls outside an admitted RPC do not consume the RPC budget.
func ReserveMemory(ctx context.Context, bytes int64) error {
	lease, _ := ctx.Value(leaseKey{}).(*memoryLease)
	if lease == nil {
		return nil
	}
	return lease.reserve(bytes)
}

func (l *memoryLease) reserve(n int64) error {
	a := l.budget
	a.mu.Lock()
	defer a.mu.Unlock()
	if n < 0 || n > math.MaxInt64-l.bytes {
		return status.Error(codes.ResourceExhausted, "invalid RPC memory reservation")
	}
	if a.total > 0 && (n > a.total-a.used || (l.download && n > a.downloadLimit-a.downloads)) {
		if a.rejected != nil {
			a.rejected.Add(context.Background(), 1, metric.WithAttributes(attribute.Bool("download", l.download)))
		}
		return status.Error(codes.ResourceExhausted, "fibre memory budget exhausted; retry later")
	}
	if a.total > 0 {
		a.used += n
		if l.download {
			a.downloads += n
		}
	}
	l.bytes += n
	return nil
}

func (l *memoryLease) release() {
	if l.refs.Add(-1) != 0 {
		return
	}
	a := l.budget
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.total > 0 {
		a.used -= l.bytes
		if l.download {
			a.downloads -= l.bytes
		}
	}
}

func responseCapacity(size int) int {
	for mem.IsBelowBufferPoolingThreshold(size) {
		size = max(1, size*2)
	}
	return size
}

// Get and Put implement mem.BufferPool to track response lifetime, without retaining buffers for reuse.
func (l *memoryLease) Get(size int) *[]byte {
	buf := make([]byte, size, responseCapacity(size))
	l.refs.Add(1)
	return &buf
}

func (l *memoryLease) Put(buf *[]byte) {
	*buf = nil
	l.release()
}

// transportReader depends on the pinned gRPC transport; its wire test must pass on upgrades.
type transportReader interface {
	ReadMessageHeader([]byte) error
	RecvCompress() string
}

type receiveTimerKey struct{}

var errReceiveTimeout = status.Error(codes.ResourceExhausted, "request receive timeout")

// Install cancellation before gRPC builds its transport reader.
func receiveTimeoutTap(ctx context.Context, _ *tap.Info) (context.Context, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	var once sync.Once
	timer := time.AfterFunc(receiveTimeout, func() { once.Do(func() { cancel(errReceiveTimeout) }) })
	stop := func() {
		// Wait for cancellation if it started; otherwise prevent it before handler work.
		once.Do(func() {})
		timer.Stop()
	}
	context.AfterFunc(ctx, stop)
	return context.WithValue(ctx, receiveTimerKey{}, stop), nil
}

func (a *Admission) receive(ctx context.Context, target any, download bool) (err error) {
	stopReceive, _ := ctx.Value(receiveTimerKey{}).(func())
	defer func() {
		if stopReceive != nil {
			stopReceive()
		}
		if context.Cause(ctx) == errReceiveTimeout {
			err = errReceiveTimeout
		}
	}()
	r, ok := grpc.ServerTransportStreamFromContext(ctx).(transportReader)
	if !ok {
		return status.Error(codes.Internal, "gRPC transport reader unavailable")
	}
	var header [5]byte
	if err := r.ReadMessageHeader(header[:]); err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	n := int64(binary.BigEndian.Uint32(header[1:]))
	limit := a.maxMessage
	if download {
		limit = maxDownloadShardRequestSize
	}
	if n > int64(limit) || header[0] > 1 {
		return status.Error(codes.ResourceExhausted, "invalid RPC message size or compression flag")
	}
	charge := n
	if header[0] == 1 {
		charge = int64(limit)
	}
	// Six payload copies cover the contiguous body, decoding and gRPC's default
	// tiny-frame backlog compaction, whose pooled capacity can approach four copies.
	// Row/proof metadata and verification views have separate protocol-bounded overhead.
	overhead := int64(a.codec.maxShardRows)*(256+64*int64(a.codec.maxProofSegments)) + 64<<10
	if download {
		overhead = 4096
	}
	if err := ReserveMemory(ctx, 6*charge+overhead); err != nil {
		return err
	}
	// Read into one allocation rather than retaining one buffer per HTTP/2 frame.
	// This also preserves gRPC's extra flow-control credit for the full message.
	body := make([]byte, int(n))
	if err := r.ReadMessageHeader(body); err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	if stopReceive != nil {
		stopReceive()
	}
	if header[0] == 1 {
		compressor := encoding.GetCompressor(r.RecvCompress())
		if compressor == nil {
			return status.Error(codes.Unimplemented, "unsupported request compression")
		}
		decoded, err := compressor.Decompress(bytes.NewReader(body))
		if err != nil {
			return status.Error(codes.Internal, err.Error())
		}
		if closer, ok := decoded.(io.Closer); ok {
			defer closer.Close()
		}
		body, err := io.ReadAll(io.LimitReader(decoded, int64(limit)+1))
		if err != nil {
			return status.Error(codes.Internal, err.Error())
		}
		if len(body) > limit {
			return status.Error(codes.ResourceExhausted, "decompressed message exceeds protocol limit")
		}
		return a.codec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(body)}, target)
	}
	return a.codec.Unmarshal(mem.BufferSlice{mem.SliceBuffer(body)}, target)
}

func (a *Admission) register(server *grpc.Server, service types.FibreServer) {
	desc := types.Fibre_serviceDesc
	desc.Methods = nil
	for _, method := range types.Fibre_serviceDesc.Methods {
		desc.Streams = append(desc.Streams, grpc.StreamDesc{StreamName: method.MethodName, Handler: func(srv any, stream grpc.ServerStream) error {
			info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/celestia.fibre.v1.Fibre/" + method.MethodName}
			_, err := recoverUnaryInterceptor(stream.Context(), nil, info, func(context.Context, any) (any, error) {
				return nil, a.serve(srv, stream, method)
			})
			return err
		}})
	}
	server.RegisterService(&desc, service)
}

func (a *Admission) serve(srv any, stream grpc.ServerStream, method grpc.MethodDesc) error {
	lease := &memoryLease{budget: a, download: method.MethodName == "DownloadShard"}
	lease.refs.Store(1)
	defer lease.release()
	ctx := context.WithValue(stream.Context(), leaseKey{}, lease)
	if err := grpc.SetSendCompressor(ctx, encoding.Identity); err != nil {
		return err
	}
	var upload *types.UploadShardRequest
	defer func() {
		if upload != nil {
			*upload = types.UploadShardRequest{}
		}
	}()
	response, err := method.Handler(srv, ctx, func(v any) error {
		upload, _ = v.(*types.UploadShardRequest)
		return a.receive(ctx, v, lease.download)
	}, nil)
	if err != nil {
		return err
	}
	msg, ok := response.(sizedBufferMarshaler)
	if !ok {
		return status.Error(codes.Internal, "unsupported response type")
	}
	if msg.Size() > a.maxMessage {
		return status.Error(codes.ResourceExhausted, "response exceeds protocol limit")
	}
	if err := lease.reserve(int64(responseCapacity(msg.Size())) + 8192); err != nil {
		return err
	}
	codec := &pooledCodec{pool: lease}
	data, err := codec.Marshal(response)
	if err != nil {
		return err
	}
	defer func() { data.Free() }()
	return stream.SendMsg(&data)
}
