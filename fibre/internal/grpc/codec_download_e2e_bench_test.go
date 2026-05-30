package grpc

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const e2eMaxMsg = 200 << 20 // 200 MiB, above MaxBlobSize

// benchFibreServer serves a fixed DownloadShard response. The server marshals
// it with whichever codec the client's content-subtype selects (proto for the
// stock path, fibre-proto scatter for the arena path — both emit identical
// bytes).
type benchFibreServer struct {
	types.UnimplementedFibreServer
	resp *types.DownloadShardResponse
}

func (s *benchFibreServer) DownloadShard(context.Context, *types.DownloadShardRequest) (*types.DownloadShardResponse, error) {
	return s.resp, nil
}

// dialE2E stands up a bufconn-backed gRPC server serving `resp` and returns a
// connected ClientConn. Exercises the full marshal→transport→unmarshal path.
func dialE2E(b *testing.B, resp *types.DownloadShardResponse) *grpclib.ClientConn {
	b.Helper()
	lis := bufconn.Listen(4 << 20)
	srv := grpclib.NewServer(grpclib.MaxRecvMsgSize(e2eMaxMsg), grpclib.MaxSendMsgSize(e2eMaxMsg))
	types.RegisterFibreServer(srv, &benchFibreServer{resp: resp})
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpclib.NewClient("passthrough://bufnet",
		grpclib.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpclib.WithTransportCredentials(insecure.NewCredentials()),
		grpclib.WithDefaultCallOptions(
			grpclib.MaxCallRecvMsgSize(e2eMaxMsg),
			grpclib.MaxCallSendMsgSize(e2eMaxMsg),
		),
	)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
	})
	return conn
}

// BenchmarkDownloadE2E_Stock measures a full DownloadShard over gRPC using the
// default proto codec — the path today.
func BenchmarkDownloadE2E_Stock(b *testing.B) {
	ctx := context.Background()
	req := &types.DownloadShardRequest{BlobId: make([]byte, 33), WithRlc: true}
	for _, s := range sizes {
		resp := makeResp(s.rows, s.rowSize)
		wire, _ := gogoproto.Marshal(resp)
		conn := dialE2E(b, resp)
		client := types.NewFibreClient(conn)
		if _, err := client.DownloadShard(ctx, req); err != nil { // warm up / dial
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("rows=%d", s.rows), func(b *testing.B) {
			b.SetBytes(int64(len(wire)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := client.DownloadShard(ctx, req); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDownloadE2E_Arena measures the same call decoded through the arena
// codec via DownloadShardInto.
func BenchmarkDownloadE2E_Arena(b *testing.B) {
	ctx := context.Background()
	req := &types.DownloadShardRequest{BlobId: make([]byte, 33), WithRlc: true}
	for _, s := range sizes {
		resp := makeResp(s.rows, s.rowSize)
		wire, _ := gogoproto.Marshal(resp)
		conn := dialE2E(b, resp)
		if err := conn.Invoke(ctx, downloadShardMethod, req, &DownloadReply{}, grpclib.CallContentSubtype(codecName)); err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("rows=%d", s.rows), func(b *testing.B) {
			b.SetBytes(int64(len(wire)))
			b.ReportAllocs()
			for b.Loop() {
				reply := &DownloadReply{}
				if err := conn.Invoke(ctx, downloadShardMethod, req, reply, grpclib.CallContentSubtype(codecName)); err != nil {
					b.Fatal(err)
				}
				reply.Free()
			}
		})
	}
}
