package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestRecoverUnaryInterceptor ensures a panic in a handler is converted into an
// Internal gRPC error rather than propagating up and crashing the process.
func TestRecoverUnaryInterceptor(t *testing.T) {
	info := &grpc.UnaryServerInfo{FullMethod: "/test/Panic"}
	panicking := func(context.Context, any) (any, error) {
		panic("boom")
	}

	require.NotPanics(t, func() {
		resp, err := recoverUnaryInterceptor(context.Background(), nil, info, panicking)
		require.Nil(t, resp)
		require.Error(t, err)
		require.Equal(t, codes.Internal, status.Code(err))
	})
}

// TestRecoverUnaryInterceptorPassthrough ensures non-panicking handlers are
// unaffected.
func TestRecoverUnaryInterceptorPassthrough(t *testing.T) {
	info := &grpc.UnaryServerInfo{FullMethod: "/test/Ok"}
	ok := func(context.Context, any) (any, error) {
		return "ok", nil
	}

	resp, err := recoverUnaryInterceptor(context.Background(), nil, info, ok)
	require.NoError(t, err)
	require.Equal(t, "ok", resp)
}
