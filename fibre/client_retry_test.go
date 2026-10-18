package fibre

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestRetryAfter(t *testing.T) {
	withRetryInfo := func(d time.Duration) error {
		st, err := status.New(grpccodes.ResourceExhausted, "storage budget exceeded").
			WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(d)})
		require.NoError(t, err)
		return st.Err()
	}

	tests := map[string]struct {
		err  error
		want time.Duration
	}{
		"nil error is not retryable":             {nil, 0},
		"plain error is not retryable":           {errors.New("boom"), 0},
		"non-ResourceExhausted is not retryable": {status.Error(grpccodes.Internal, "x"), 0},
		"ResourceExhausted without hint":         {status.Error(grpccodes.ResourceExhausted, "full"), defaultRetryDelay},
		"ResourceExhausted with hint":            {withRetryInfo(3 * time.Second), 3 * time.Second},
		"ResourceExhausted with zero hint":       {withRetryInfo(0), defaultRetryDelay},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.want, retryAfter(tt.err))
		})
	}
}
