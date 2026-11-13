package tx_test

import (
	"context"
	"net"
	"runtime"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app/grpc/tx"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protowire"
)

// captureServer records the service registered by tx.RegisterTxService.
type captureServer struct {
	desc *grpc.ServiceDesc
	impl any
}

func (c *captureServer) RegisterService(desc *grpc.ServiceDesc, impl any) {
	c.desc, c.impl = desc, impl
}

// invokeTxStatusBatch decodes raw with the SDK gRPC codec through the
// registered TxStatusBatch handler, as the gRPC server does.
func invokeTxStatusBatch(t testing.TB, raw []byte) (any, error) {
	t.Helper()
	return invokeTxStatusBatchWithInterceptor(t, raw, nil)
}

// invokeTxStatusBatchWithInterceptor is invokeTxStatusBatch with a unary
// interceptor, as the app's gRPC server installs one.
func invokeTxStatusBatchWithInterceptor(t testing.TB, raw []byte, interceptor grpc.UnaryServerInterceptor) (any, error) {
	t.Helper()
	registry := codectypes.NewInterfaceRegistry()
	srv := &captureServer{}
	tx.RegisterTxService(srv, client.Context{}.WithClient(&stubNode{}), registry)

	grpcCodec := codec.NewProtoCodec(registry).GRPCCodec()
	for _, m := range srv.desc.Methods {
		if m.MethodName == "TxStatusBatch" {
			return m.Handler(srv.impl, context.Background(), func(v any) error {
				return grpcCodec.Unmarshal(raw, v)
			}, interceptor)
		}
	}
	t.Fatal("TxStatusBatch not registered")
	return nil, nil
}

// encodeTxIDs encodes a TxStatusBatchRequest carrying n copies of id.
func encodeTxIDs(n int, id string) []byte {
	var b []byte
	for range n {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, id)
	}
	return b
}

func TestTxStatusBatchDecodeAcceptsLimit(t *testing.T) {
	res, err := invokeTxStatusBatch(t, encodeTxIDs(20, "aa"))
	require.NoError(t, err)
	require.Len(t, res.(*tx.TxStatusBatchResponse).Statuses, 20)
}

func TestTxStatusBatchDecodeRejectsOverLimit(t *testing.T) {
	_, err := invokeTxStatusBatch(t, encodeTxIDs(21, "aa"))
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestTxStatusBatchDecodeAcceptsUnknownFields(t *testing.T) {
	raw := protowire.AppendTag(nil, 5, protowire.StartGroupType)
	raw = protowire.AppendTag(raw, 1, protowire.BytesType)
	raw = protowire.AppendString(raw, "ignored")
	raw = protowire.AppendTag(raw, 5, protowire.EndGroupType)
	raw = protowire.AppendTag(raw, 6, protowire.VarintType)
	raw = protowire.AppendVarint(raw, 7)
	raw = append(raw, encodeTxIDs(20, "aa")...)

	res, err := invokeTxStatusBatch(t, raw)
	require.NoError(t, err)
	require.Len(t, res.(*tx.TxStatusBatchResponse).Statuses, 20)
}

// TestTxStatusBatchDecodeRejectsThroughInterceptor checks that rejected
// requests still pass through the unary interceptor, so it can observe them.
func TestTxStatusBatchDecodeRejectsThroughInterceptor(t *testing.T) {
	calls := 0
	interceptor := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		calls++
		require.Equal(t, "/celestia.core.v1.tx.Tx/TxStatusBatch", info.FullMethod)
		return handler(ctx, req)
	}

	_, err := invokeTxStatusBatchWithInterceptor(t, encodeTxIDs(21, "aa"), interceptor)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, 1, calls)

	res, err := invokeTxStatusBatchWithInterceptor(t, encodeTxIDs(2, "aa"), interceptor)
	require.NoError(t, err)
	require.Len(t, res.(*tx.TxStatusBatchResponse).Statuses, 2)
	require.Equal(t, 2, calls)
}

func TestTxStatusBatchDecodeRejectsMalformed(t *testing.T) {
	_, err := invokeTxStatusBatch(t, []byte{0x0a, 0x05, 'a'})
	require.Error(t, err)
}

// TestTxStatusBatchDecodeBoundsAllocation checks that a request with a huge
// number of tx ids is rejected without allocating memory proportional to it.
func TestTxStatusBatchDecodeBoundsAllocation(t *testing.T) {
	const entries = 1_000_000
	// A group whose end tag names another field is skipped by the generated
	// decoder but is not valid protobuf.
	mismatchedGroup := protowire.AppendTag(nil, 2, protowire.StartGroupType)
	mismatchedGroup = protowire.AppendTag(mismatchedGroup, 3, protowire.EndGroupType)

	for name, raw := range map[string][]byte{
		"tx ids":                    encodeTxIDs(entries, ""),
		"mismatched group then ids": append(mismatchedGroup, encodeTxIDs(entries, "")...),
	} {
		t.Run(name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			_, err := invokeTxStatusBatch(t, raw)
			runtime.ReadMemStats(&after)

			require.Equal(t, codes.InvalidArgument, status.Code(err))
			// Decoding every entry would allocate at least 16 bytes per string
			// header.
			allocated := after.TotalAlloc - before.TotalAlloc
			require.Less(t, allocated, uint64(1<<20), "allocated %d bytes", allocated)
		})
	}
}

// TestTxStatusBatchOverGRPC checks the limit through a gRPC server using the
// SDK codec, including that rejected requests keep the InvalidArgument code.
func TestTxStatusBatchOverGRPC(t *testing.T) {
	registry := codectypes.NewInterfaceRegistry()
	srv := grpc.NewServer(grpc.ForceServerCodec(codec.NewProtoCodec(registry).GRPCCodec()))
	tx.RegisterTxService(srv, client.Context{}.WithClient(&stubNode{}), registry)
	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	txClient := tx.NewTxClient(conn)

	ids := make([]string, 21)
	for i := range ids {
		ids[i] = "aa"
	}
	res, err := txClient.TxStatusBatch(context.Background(), &tx.TxStatusBatchRequest{TxIds: ids[:20]})
	require.NoError(t, err)
	require.Len(t, res.Statuses, 20)

	_, err = txClient.TxStatusBatch(context.Background(), &tx.TxStatusBatchRequest{TxIds: ids})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
