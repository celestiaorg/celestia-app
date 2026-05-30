package grpc

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	gogoproto "github.com/cosmos/gogoproto/proto"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// benchUploadServer accepts an upload and returns immediately — the handler
// does no work, so the measured delta is the marshal (client) + unmarshal
// (server) codec path, not verification or storage.
type benchUploadServer struct {
	types.UnimplementedFibreServer
}

func (benchUploadServer) UploadShard(context.Context, *types.UploadShardRequest) (*types.UploadShardResponse, error) {
	return &types.UploadShardResponse{}, nil
}

func makeUploadReq(rows, rowSize int) *types.UploadShardRequest {
	return &types.UploadShardRequest{
		Promise: &types.PaymentPromise{
			ChainId:           "bench",
			Height:            42,
			Namespace:         bytes.Repeat([]byte{0x01}, 29),
			Commitment:        bytes.Repeat([]byte{0x02}, 32),
			CreationTimestamp: time.Unix(1700000000, 0).UTC(),
			SignerPublicKey:   secp256k1.PubKey{Key: bytes.Repeat([]byte{0x03}, 33)},
			Signature:         bytes.Repeat([]byte{0x04}, 64),
		},
		Shard: makeResp(rows, rowSize).Shard,
	}
}

func dialUploadE2E(b *testing.B) *grpclib.ClientConn {
	b.Helper()
	lis := bufconn.Listen(4 << 20)
	srv := grpclib.NewServer(grpclib.MaxRecvMsgSize(e2eMaxMsg), grpclib.MaxSendMsgSize(e2eMaxMsg))
	types.RegisterFibreServer(srv, &benchUploadServer{})
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

// BenchmarkUploadE2E_Stock measures a full UploadShard over gRPC using the
// default proto codec — contiguous marshal on the client, Materialize +
// per-row gogoproto unmarshal on the server.
func BenchmarkUploadE2E_Stock(b *testing.B) {
	ctx := context.Background()
	conn := dialUploadE2E(b)
	client := types.NewFibreClient(conn)
	for _, s := range sizes {
		req := makeUploadReq(s.rows, s.rowSize)
		wire, _ := gogoproto.Marshal(req)
		if _, err := client.UploadShard(ctx, req); err != nil { // warm up / dial
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("rows=%d", s.rows), func(b *testing.B) {
			b.SetBytes(int64(len(wire)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := client.UploadShard(ctx, req); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkUploadE2E_Arena measures the same call over the fibre-proto codec:
// scatter marshal on the client, arena decode on the server.
func BenchmarkUploadE2E_Arena(b *testing.B) {
	ctx := context.Background()
	conn := dialUploadE2E(b)
	client := types.NewFibreClient(conn)
	opt := grpclib.CallContentSubtype(codecName)
	for _, s := range sizes {
		req := makeUploadReq(s.rows, s.rowSize)
		wire, _ := gogoproto.Marshal(req)
		if _, err := client.UploadShard(ctx, req, opt); err != nil { // warm up / dial
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("rows=%d", s.rows), func(b *testing.B) {
			b.SetBytes(int64(len(wire)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := client.UploadShard(ctx, req, opt); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
