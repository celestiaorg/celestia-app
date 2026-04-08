package grpc

import (
	"context"
	"io"

	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	core "github.com/cometbft/cometbft/types"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client combines [FibreClient] with [io.Closer] to manage the lifecycle
// of both the client and its underlying connection.
type Client interface {
	types.FibreClient
	io.Closer
}

// NewClientFn is a constructor function that creates a [Client]
// for a given validator. It should handle host resolution and connection establishment.
type NewClientFn func(ctx context.Context, val *core.Validator) (Client, error)

// fibreClientCloser wraps a [FibreClient] and [grpclib.ClientConn] to implement [Client].
type fibreClientCloser struct {
	types.FibreClient
	conn *grpclib.ClientConn
}

func (f *fibreClientCloser) Close() error {
	return f.conn.Close()
}

// DefaultNewClientFn returns the default [NewClientFn] that uses the provided
// [validator.HostRegistry] to resolve validator hosts and establishes insecure gRPC connections
// with OpenTelemetry instrumentation for distributed tracing.
// The maxMsgSize parameter sets the maximum gRPC message size for send and receive operations.
func DefaultNewClientFn(hostReg validator.HostRegistry, maxMsgSize int) NewClientFn {
	return func(ctx context.Context, val *core.Validator) (Client, error) {
		host, err := hostReg.GetHost(ctx, val)
		if err != nil {
			return nil, err
		}

		// TODO(@Wondertan): setup secure connection
		conn, err := grpclib.NewClient(host.String(),
			grpclib.WithTransportCredentials(insecure.NewCredentials()),
			grpclib.WithStatsHandler(otelgrpc.NewClientHandler()),
			grpclib.WithDefaultCallOptions(
				grpclib.MaxCallRecvMsgSize(maxMsgSize),
				grpclib.MaxCallSendMsgSize(maxMsgSize),
			),
		)
		if err != nil {
			return nil, err
		}

		return &fibreClientCloser{
			FibreClient: types.NewFibreClient(conn),
			conn:        conn,
		}, nil
	}
}
