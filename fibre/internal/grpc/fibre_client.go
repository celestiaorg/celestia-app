package grpc

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"

	"github.com/celestiaorg/celestia-app/v9/fibre/internal/tlsid"
	"github.com/celestiaorg/celestia-app/v9/fibre/validator"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	core "github.com/cometbft/cometbft/types"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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

// DefaultNewClientFn returns the default [NewClientFn]. It resolves the
// validator's network host through hostReg, then dials over TLS with the
// peer identity bound to the validator's consensus pubkey via
// [tlsid.VerifyConnection].
//
// log records TLS identity-verification failures with peer context. Because
// grpc.NewClient dials lazily, verification runs on the first RPC and a failure
// otherwise surfaces upstream only as an aggregated quorum/unavailable error;
// logging here preserves the underlying cause for operators.
func DefaultNewClientFn(hostReg validator.HostRegistry, maxMsgSize int, log *slog.Logger) NewClientFn {
	if log == nil {
		log = slog.Default()
	}
	return func(ctx context.Context, val *core.Validator) (Client, error) {
		host, err := hostReg.GetHost(ctx, val)
		if err != nil {
			return nil, err
		}
		if val.PubKey == nil {
			return nil, errors.New("validator has no consensus pubkey for TLS identity check")
		}

		verify := tlsid.VerifyConnection(val.PubKey)
		tlsCfg := &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // identity is verified via VerifyConnection
			VerifyConnection: func(state tls.ConnectionState) error {
				if err := verify(state); err != nil {
					log.Warn("fibre TLS peer identity verification failed",
						"validator", val.Address.String(),
						"host", host.String(),
						"err", err)
					return err
				}
				return nil
			},
			MinVersion: tls.VersionTLS13,
		}

		conn, err := grpclib.NewClient(host.String(),
			grpclib.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
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
