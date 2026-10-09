package grpc

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"
)

type queuedResponseService struct{ types.UnimplementedFibreServer }

func (*queuedResponseService) UploadShard(context.Context, *types.UploadShardRequest) (*types.UploadShardResponse, error) {
	return &types.UploadShardResponse{ValidatorSignature: make([]byte, 64)}, nil
}

func (*queuedResponseService) DownloadShard(context.Context, *types.DownloadShardRequest) (*types.DownloadShardResponse, error) {
	return &types.DownloadShardResponse{Shard: &types.BlobShard{Rows: []*types.BlobRow{{Data: make([]byte, 8<<20)}}}}, nil
}

type queuedResponseStats struct{ rpcEnd, connEnd chan struct{} }

func (*queuedResponseStats) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (*queuedResponseStats) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return ctx
}

func (s *queuedResponseStats) HandleConn(_ context.Context, event stats.ConnStats) {
	if _, ok := event.(*stats.ConnEnd); ok {
		close(s.connEnd)
	}
}

func (s *queuedResponseStats) HandleRPC(_ context.Context, event stats.RPCStats) {
	if _, ok := event.(*stats.End); ok {
		close(s.rpcEnd)
	}
}

func TestQueuedResponseReleasesBudget(t *testing.T) {
	for _, method := range []string{"UploadShard", "DownloadShard"} {
		for _, mode := range []string{"read", "disconnect"} {
			t.Run(method+"/"+mode, func(t *testing.T) {
				admission := NewAdmission(256<<20, 16<<20, 4096, 14)
				observer := &queuedResponseStats{rpcEnd: make(chan struct{}), connEnd: make(chan struct{})}
				server := grpc.NewServer(grpc.ForceServerCodecV2(admission.codec), grpc.InTapHandle(receiveTimeoutTap), grpc.StatsHandler(connectionBudgetStats{}), grpc.StatsHandler(observer), grpc.MaxSendMsgSize(16<<20))
				admission.register(server, &queuedResponseService{}, nil)
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				require.NoError(t, err)
				served := make(chan struct{})
				go func() { defer close(served); _ = server.Serve(listener) }()
				t.Cleanup(func() { server.Stop(); <-served })
				conn, err := net.Dial("tcp", listener.Addr().String())
				require.NoError(t, err)
				defer conn.Close()
				require.NoError(t, conn.SetDeadline(time.Now().Add(10*time.Second)))
				_, err = io.WriteString(conn, http2.ClientPreface)
				require.NoError(t, err)
				framer := http2.NewFramer(conn, conn)
				framer.ReadMetaHeaders = hpack.NewDecoder(4096, nil)
				require.NoError(t, framer.WriteSettings(http2.Setting{ID: http2.SettingInitialWindowSize, Val: 0}))
				var headers bytes.Buffer
				encoder := hpack.NewEncoder(&headers)
				for _, field := range []hpack.HeaderField{
					{Name: ":method", Value: "POST"},
					{Name: ":scheme", Value: "http"},
					{Name: ":path", Value: "/celestia.fibre.v1.Fibre/" + method},
					{Name: ":authority", Value: listener.Addr().String()},
					{Name: "content-type", Value: "application/grpc+fibre-proto"},
					{Name: "te", Value: "trailers"},
				} {
					require.NoError(t, encoder.WriteField(field))
				}
				require.NoError(t, framer.WriteHeaders(http2.HeadersFrameParam{StreamID: 1, BlockFragment: headers.Bytes(), EndHeaders: true}))
				require.NoError(t, framer.WriteData(1, true, []byte{0, 0, 0, 0, 0}))
				select {
				case <-observer.rpcEnd:
				case <-time.After(5 * time.Second):
					t.Fatal("RPC did not finish queuing its response")
				}
				// The ping ACK confirms loopy processed the previously queued response.
				require.NoError(t, framer.WritePing(false, [8]byte{1}))
				for {
					frame, err := framer.ReadFrame()
					require.NoError(t, err)
					if ping, ok := frame.(*http2.PingFrame); ok && ping.IsAck() && ping.Data == [8]byte{1} {
						break
					}
				}
				used := func() int64 { admission.mu.Lock(); defer admission.mu.Unlock(); return admission.used }
				require.Positive(t, used(), "queued response must retain its reservation")
				if mode == "read" {
					require.NoError(t, framer.WriteWindowUpdate(0, 16<<20))
					require.NoError(t, framer.WriteWindowUpdate(1, 16<<20))
					for {
						frame, err := framer.ReadFrame()
						require.NoError(t, err)
						if headers, ok := frame.(*http2.MetaHeadersFrame); ok && headers.StreamEnded() {
							break
						}
					}
					require.Eventually(t, func() bool { return used() == 0 }, time.Second, time.Millisecond,
						"reading the response must release its reservation before disconnect")
				}
				require.NoError(t, conn.Close())
				select {
				case <-observer.connEnd:
				case <-time.After(5 * time.Second):
					t.Fatal("connection did not close")
				}
				require.Eventually(t, func() bool { return used() == 0 }, 3*time.Second, 10*time.Millisecond, "reservation remains after ConnEnd: %d bytes", used())
			})
		}
	}
}

func TestConnectionCloseRetainsHandlerReservation(t *testing.T) {
	observer := connectionBudgetStats{}
	ctx := observer.TagConn(t.Context(), &stats.ConnTagInfo{})
	budget := newMemoryBudget(100)
	lease := newMemoryLease(budget, true)
	lease.conn = ctx.Value(connectionBudgetKey{}).(*connectionBudget)
	lease.conn.handlers = 1
	require.NoError(t, lease.reserve(ctx, 75))
	queued := newMemoryLease(budget, false)
	queued.conn = lease.conn
	require.NoError(t, queued.reserve(ctx, 25))
	abandoned := queued.Get(1)
	queued.release()
	first, second := lease.Get(1), lease.Get(1)
	var wg sync.WaitGroup
	wg.Go(func() { observer.HandleConn(ctx, &stats.ConnEnd{}) })
	wg.Go(func() { lease.Put(first) })
	wg.Wait()
	require.EqualValues(t, 100, budget.used, "a running handler retains the transport and its queued responses")
	lease.Put(second)
	lease.Put(lease.Get(1)) // A handler may finish marshalling after ConnEnd.
	lease.conn.releaseHandler()
	lease.release()
	queued.Put(abandoned)
	observer.HandleConn(ctx, &stats.ConnEnd{})
	require.Zero(t, budget.used)
	require.Zero(t, budget.downloads)
	require.Zero(t, lease.refs.Load(), "late Put and repeated ConnEnd must not release twice")
	require.Empty(t, lease.conn.leases)
}
