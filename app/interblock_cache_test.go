package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mapAppOptions map[string]any

func (m mapAppOptions) Get(key string) any { return m[key] }

func TestParseInterBlockCacheConfig(t *testing.T) {
	valid := []struct {
		name     string
		opts     mapAppOptions
		syncOnly bool
		size     uint
	}{
		{"unset", mapAppOptions{}, false, DefaultInterBlockCacheSize},
		{"empty", mapAppOptions{FlagInterBlockCache: ""}, false, DefaultInterBlockCacheSize},
		{"off", mapAppOptions{FlagInterBlockCache: "off"}, false, DefaultInterBlockCacheSize},
		{"sync-only", mapAppOptions{FlagInterBlockCache: "sync-only"}, true, DefaultInterBlockCacheSize},
		{"case and spaces", mapAppOptions{FlagInterBlockCache: " Sync-Only "}, true, DefaultInterBlockCacheSize},
		{"size int", mapAppOptions{FlagInterBlockCache: "sync-only", FlagInterBlockCacheSize: 5000}, true, 5000},
		{"size int64 from toml", mapAppOptions{FlagInterBlockCache: "sync-only", FlagInterBlockCacheSize: int64(7)}, true, 7},
		{"size string from env", mapAppOptions{FlagInterBlockCache: "sync-only", FlagInterBlockCacheSize: "20000"}, true, 20000},
		{"size ignored when off", mapAppOptions{FlagInterBlockCache: "off", FlagInterBlockCacheSize: "many"}, false, DefaultInterBlockCacheSize},
	}
	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ParseInterBlockCacheConfig(tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.syncOnly, cfg.SyncOnly)
			require.Equal(t, tc.size, cfg.Size)
		})
	}

	cfg, err := ParseInterBlockCacheConfig(nil)
	require.NoError(t, err)
	require.False(t, cfg.SyncOnly)

	invalid := []struct {
		name string
		opts mapAppOptions
	}{
		{"unknown mode", mapAppOptions{FlagInterBlockCache: "on"}},
		{"bool mode", mapAppOptions{FlagInterBlockCache: true}},
		{"list mode", mapAppOptions{FlagInterBlockCache: []string{"sync-only"}}},
		{"zero size", mapAppOptions{FlagInterBlockCache: "sync-only", FlagInterBlockCacheSize: 0}},
		{"negative size", mapAppOptions{FlagInterBlockCache: "sync-only", FlagInterBlockCacheSize: -1}},
		{"huge size", mapAppOptions{FlagInterBlockCache: "sync-only", FlagInterBlockCacheSize: int64(1) << 40}},
		{"non-integer size", mapAppOptions{FlagInterBlockCache: "sync-only", FlagInterBlockCacheSize: "many"}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseInterBlockCacheConfig(tc.opts)
			require.Error(t, err)
		})
	}
}

func TestWithInterBlockCache(t *testing.T) {
	require.Len(t, withInterBlockCache(InterBlockCacheConfig{Size: 1}, nil), 0)
	require.Len(t, withInterBlockCache(InterBlockCacheConfig{SyncOnly: true, Size: 1}, nil), 1)
}

type captureGRPCServer struct{ descs []*grpc.ServiceDesc }

func (c *captureGRPCServer) RegisterService(sd *grpc.ServiceDesc, _ any) {
	c.descs = append(c.descs, sd)
}

func TestRefusingGRPCServer(t *testing.T) {
	called := false
	desc := &grpc.ServiceDesc{
		ServiceName: "test.Service",
		Methods: []grpc.MethodDesc{{
			MethodName: "Get",
			Handler: func(any, context.Context, func(any) error, grpc.UnaryServerInterceptor) (any, error) {
				called = true
				return nil, errors.New("ran")
			},
		}},
		Streams: []grpc.StreamDesc{{
			StreamName: "Watch",
			Handler: func(any, grpc.ServerStream) error {
				called = true
				return errors.New("ran")
			},
		}},
	}
	capture := &captureGRPCServer{}
	refusingGRPCServer{Server: capture}.RegisterService(desc, nil)
	require.Len(t, capture.descs, 1)
	got := capture.descs[0]
	require.Equal(t, "test.Service", got.ServiceName)
	require.Equal(t, "Get", got.Methods[0].MethodName)
	require.Equal(t, "Watch", got.Streams[0].StreamName)

	_, err := got.Methods[0].Handler(nil, context.Background(), func(any) error { return nil }, nil)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Equal(t, codes.Unavailable, status.Code(got.Streams[0].Handler(nil, nil)))
	require.False(t, called)
	// the caller's descriptor is left untouched
	_, err = desc.Methods[0].Handler(nil, context.Background(), nil, nil)
	require.EqualError(t, err, "ran")
}
