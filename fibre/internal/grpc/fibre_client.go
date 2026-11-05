package grpc

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/celestiaorg/celestia-app/v11/fibre/internal/tlsid"
	"github.com/celestiaorg/celestia-app/v11/fibre/validator"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	core "github.com/cometbft/cometbft/types"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
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

// Connect starts connecting without waiting for an RPC.
func (f *fibreClientCloser) Connect() {
	f.conn.Connect()
}

// WaitReady connects and waits until the connection is ready or ctx is done.
func (f *fibreClientCloser) WaitReady(ctx context.Context) bool {
	f.conn.Connect()
	for {
		s := f.conn.GetState()
		if s == connectivity.Ready {
			return true
		}
		if !f.conn.WaitForStateChange(ctx, s) {
			return false
		}
	}
}

// ClientDialOption configures the dialer returned by [DefaultNewClientFn].
type ClientDialOption func(*clientDialConfig)

type clientDialConfig struct {
	forceTLS bool
	observe  DialObserver
}

// WithForceTLS makes the dialer always use TLS and never probe plaintext
// HTTP/2. Passing false keeps the default plaintext-first behaviour.
func WithForceTLS(force bool) ClientDialOption {
	return func(c *clientDialConfig) { c.forceTLS = force }
}

// WithDialObserver registers o to observe the transport mode of each dial. A
// nil observer is ignored.
func WithDialObserver(o DialObserver) ClientDialOption {
	return func(c *clientDialConfig) { c.observe = o }
}

// DefaultNewClientFn returns the default [NewClientFn]. It resolves the
// validator's network host through hostReg, then dials over plaintext HTTP/2,
// falling back to TLS with the peer identity bound to the validator's consensus
// pubkey via [tlsid.VerifyConnection] for servers that only speak TLS. Pass
// [WithForceTLS] to always dial over TLS.
//
// log records TLS identity-verification failures with peer context. Because
// grpc.NewClient dials lazily, verification runs on the first RPC and a failure
// otherwise surfaces upstream only as an aggregated quorum/unavailable error;
// logging here preserves the underlying cause for operators.
//
// chainID is evaluated lazily per dial because the state client resolves it
// during Start, which can run after this constructor.
func DefaultNewClientFn(hostReg validator.HostRegistry, chainID func() string, maxMsgSize int, log *slog.Logger, opts ...ClientDialOption) NewClientFn {
	if log == nil {
		log = slog.Default()
	}
	if chainID == nil {
		chainID = func() string { return "" }
	}
	var dialCfg clientDialConfig
	for _, opt := range opts {
		opt(&dialCfg)
	}
	modes := newTransportModes()
	return func(ctx context.Context, val *core.Validator) (Client, error) {
		host, err := hostReg.GetHost(ctx, val)
		if err != nil {
			return nil, err
		}
		if val.PubKey == nil {
			return nil, errors.New("validator has no consensus pubkey for TLS identity check")
		}
		cid := chainID()
		if cid == "" {
			return nil, errors.New("chain ID is empty; state client must be started before dialing")
		}

		verify := tlsid.VerifyConnection(val.PubKey, cid)
		tlsCfg := &tls.Config{
			// The peer certificate is self-signed; VerifyConnection below authenticates the
			// validator consensus-key endorsement instead of a CA/SAN chain.
			// codeql[go/disabled-certificate-check]
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

		transport := &autoTransport{
			modes:    modes,
			host:     host.String(),
			forceTLS: dialCfg.forceTLS,
			observe:  dialCfg.observe,
		}
		transportOpts := transport.dialOptions(batchingCreds{credentials.NewTLS(tlsCfg)})
		opts := make([]grpclib.DialOption, 0, 4+len(transportOpts))
		opts = append(opts,
			grpclib.WithIdleTimeout(20*time.Second),
			grpclib.WithWriteBufferSize(uploadWriteBufferSize),
			grpclib.WithStatsHandler(otelgrpc.NewClientHandler()),
			grpclib.WithDefaultCallOptions(
				grpclib.MaxCallRecvMsgSize(maxMsgSize),
				grpclib.MaxCallSendMsgSize(maxMsgSize),
				// Pooled + scatter-gather codec; see codec.go.
				grpclib.CallContentSubtype(codecName),
			),
		)
		opts = append(opts, transportOpts...)

		conn, err := grpclib.NewClient(host.String(), opts...)
		if err != nil {
			return nil, err
		}

		return &fibreClientCloser{
			FibreClient: types.NewFibreClient(conn),
			conn:        conn,
		}, nil
	}
}
