package grpc

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/status"
)

// rpcAdmission reserves part of the total RPC capacity for uploads.
// Uploads can use all capacity; downloads cannot use the upload reserve.
type rpcAdmission struct {
	mu                             sync.Mutex
	disabled                       bool
	activeUploads, activeDownloads int
	maxRPCs, reservedUploadSlots   int
}

func (a *rpcAdmission) acquire(methodName string) (*rpcLease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var active *int
	switch methodName {
	case "UploadShard":
		active = &a.activeUploads
	case "DownloadShard":
		active = &a.activeDownloads
	default:
		return nil, status.Error(codes.Unimplemented, "unknown Fibre RPC method")
	}
	if !a.disabled {
		if a.activeUploads+a.activeDownloads >= a.maxRPCs {
			return nil, status.Error(codes.ResourceExhausted, "fibre server busy; retry later")
		}
		if methodName == "DownloadShard" && a.activeDownloads >= a.maxRPCs-a.reservedUploadSlots {
			return nil, status.Error(codes.ResourceExhausted, "fibre download capacity full; retry later")
		}
	}
	*active += 1
	lease := &rpcLease{onLastRelease: func() {
		a.mu.Lock()
		*active -= 1
		a.mu.Unlock()
	}}
	lease.refs.Store(1)
	return lease, nil
}

// rpcLease holds one admission slot until its handler and response-buffer owners finish.
type rpcLease struct {
	refs          atomic.Int32
	onLastRelease func()
}

// retain adds an owner of the same admission slot, not another slot.
func (l *rpcLease) retain() { l.refs.Add(1) }

func (l *rpcLease) release() {
	if l.refs.Add(-1) == 0 {
		l.onLastRelease()
	}
}

// Get allocates a response buffer and adds an owner to the lease.
func (l *rpcLease) Get(size int) *[]byte {
	capacity := size
	// Small buffers otherwise bypass BufferPool.Put, which would leak the lease.
	for mem.IsBelowBufferPoolingThreshold(capacity) {
		capacity = max(1, capacity*2)
	}
	buf := make([]byte, size, capacity)
	l.retain()
	return &buf
}

// Put discards the buffer and releases its ownership when gRPC frees its last reference.
func (l *rpcLease) Put(buf *[]byte) {
	*buf = nil
	l.release()
}

// register keeps unary cardinality and wire messages, but acquires before RecvMsg.
func (a *rpcAdmission) register(server *grpc.Server, service types.FibreServer) {
	desc := types.Fibre_serviceDesc
	// Remove unary registrations so gRPC cannot bypass admission. The loop adds their stream-handler replacements.
	desc.Methods = nil
	for _, method := range types.Fibre_serviceDesc.Methods {
		desc.Streams = append(desc.Streams, grpc.StreamDesc{
			StreamName: method.MethodName,
			Handler: func(srv any, stream grpc.ServerStream) error {
				info := &grpc.UnaryServerInfo{
					Server:     srv,
					FullMethod: "/" + desc.ServiceName + "/" + method.MethodName,
				}
				_, err := recoverUnaryInterceptor(
					stream.Context(), nil, info,
					func(context.Context, any) (any, error) {
						return nil, a.serve(srv, stream, method)
					},
				)
				return err
			},
		})
	}
	server.RegisterService(&desc, service)
}

func (a *rpcAdmission) serve(service any, stream grpc.ServerStream, method grpc.MethodDesc) error {
	lease, err := a.acquire(method.MethodName)
	if err != nil {
		return err
	}
	defer lease.release()
	// Send all responses uncompressed; compressed incoming requests are still accepted.
	// Compression could release the tracked buffer while its replacement remains queued.
	if err := grpc.SetSendCompressor(stream.Context(), encoding.Identity); err != nil {
		return err
	}
	var upload *types.UploadShardRequest
	defer func() {
		if upload != nil {
			*upload = types.UploadShardRequest{}
		}
	}()
	// Add future unary interceptors to the final argument below, after admission and decoding.
	// grpc.UnaryInterceptor server options do not run for these stream registrations.
	response, err := method.Handler(service, stream.Context(), func(v any) error {
		upload, _ = v.(*types.UploadShardRequest)
		return stream.RecvMsg(v)
	}, nil)
	if err != nil {
		return err
	}
	codec := &pooledCodec{pool: lease}
	data, err := codec.Marshal(response)
	if err != nil {
		return err
	}
	// SendMsg transfers ownership through the codec. Free any untransferred buffers on error.
	defer func() { data.Free() }()
	return stream.SendMsg(&data)
}
