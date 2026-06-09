package txsim_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/test/txsim"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsContextEnded(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "context canceled",
			err:  context.Canceled,
			want: true,
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "wrapped context deadline exceeded",
			err:  fmt.Errorf("sequence 0: %w", context.DeadlineExceeded),
			want: true,
		},
		{
			// This is the gRPC status error observed in the flaky
			// TestTxsimDefaultKeypath failure when the controlling context's
			// deadline fires during an in-flight gRPC call. It does NOT satisfy
			// errors.Is(err, context.DeadlineExceeded).
			name: "grpc deadline exceeded status",
			err:  status.Error(codes.DeadlineExceeded, "stream terminated by RST_STREAM with error code: CANCEL"),
			want: true,
		},
		{
			name: "wrapped grpc deadline exceeded status",
			err:  fmt.Errorf("sequence 0: %w", status.Error(codes.DeadlineExceeded, "stream terminated by RST_STREAM with error code: CANCEL")),
			want: true,
		},
		{
			name: "grpc canceled status",
			err:  status.Error(codes.Canceled, "context canceled"),
			want: true,
		},
		{
			name: "unrelated grpc error",
			err:  status.Error(codes.NotFound, "tx not found"),
			want: false,
		},
		{
			name: "unrelated error",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, txsim.IsContextEnded(tc.err))
		})
	}
}
