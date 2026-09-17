package sign

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/encoding"
	privvalproto "github.com/cometbft/cometbft/proto/tendermint/privval"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const signTimeout = 5 * time.Second

// TLSConfig holds PEM file paths for mutual TLS to the PrivValidatorAPI
// endpoint. A nil or empty config means plaintext transport. All three
// files are required otherwise.
type TLSConfig struct {
	// CAFile is the CA certificate used to verify the server certificate.
	CAFile string
	// CertFile is the client certificate presented to the server.
	CertFile string
	// KeyFile is the private key for the client certificate.
	KeyFile string
}

// Empty reports whether no TLS files are configured.
func (c *TLSConfig) Empty() bool {
	return c == nil || (c.CAFile == "" && c.CertFile == "" && c.KeyFile == "")
}

// credentials builds gRPC transport credentials from the configured files.
func (c *TLSConfig) credentials() (credentials.TransportCredentials, error) {
	caPEM, err := os.ReadFile(c.CAFile)
	if err != nil {
		return nil, fmt.Errorf("reading CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no certificates found in CA file %s", c.CAFile)
	}

	tlsCfg := &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS13,
	}
	cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("loading client certificate: %w", err)
	}
	tlsCfg.Certificates = []tls.Certificate{cert}
	return credentials.NewTLS(tlsCfg), nil
}

// GRPCClient implements [types.PrivValidator] by connecting to a node's
// PrivValidatorAPI gRPC endpoint. This allows fiber to sign payment promises
// via the node's privval without needing an external signer like tmkms.
type GRPCClient struct {
	conn    *grpc.ClientConn
	client  privvalproto.PrivValidatorAPIClient
	chainID string
	log     *slog.Logger
}

var (
	_ types.PrivValidator = (*GRPCClient)(nil)
	_ io.Closer           = (*GRPCClient)(nil)
)

// NewGRPCClient dials the given gRPC address and returns a client that
// delegates signing to the remote PrivValidatorAPI. If tlsCfg is nil or
// empty, it dials with insecure credentials (intended for localhost use);
// otherwise it uses mutual TLS with the configured PEM files.
func NewGRPCClient(addr string, chainID string, tlsCfg *TLSConfig, log *slog.Logger) (*GRPCClient, error) {
	creds := insecure.NewCredentials()
	transport := "plaintext"
	if !tlsCfg.Empty() {
		var err error
		creds, err = tlsCfg.credentials()
		if err != nil {
			return nil, fmt.Errorf("privval gRPC TLS config: %w", err)
		}
		transport = "mtls"
	}
	log.Info("connecting to privval gRPC signer", "addr", addr, "transport", transport)

	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("dialing privval gRPC at %s: %w", addr, err)
	}

	return &GRPCClient{
		conn:    conn,
		client:  privvalproto.NewPrivValidatorAPIClient(conn),
		chainID: chainID,
		log:     log,
	}, nil
}

// GetPubKey fetches the public key from the remote PrivValidatorAPI.
func (g *GRPCClient) GetPubKey() (crypto.PubKey, error) {
	ctx, cancel := context.WithTimeout(context.Background(), signTimeout)
	defer cancel()

	resp, err := g.client.GetPubKey(ctx, &privvalproto.PubKeyRequest{ChainId: g.chainID})
	if err != nil {
		return nil, fmt.Errorf("grpc GetPubKey: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("remote signer error: %s", resp.Error.Description)
	}

	pk, err := encoding.PubKeyFromProto(resp.PubKey)
	if err != nil {
		return nil, fmt.Errorf("decoding public key from proto: %w", err)
	}
	return pk, nil
}

// SignRawBytes delegates signing to the remote PrivValidatorAPI gRPC endpoint.
func (g *GRPCClient) SignRawBytes(chainID, uniqueID string, rawBytes []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), signTimeout)
	defer cancel()

	resp, err := g.client.SignRawBytes(ctx, &privvalproto.SignRawBytesRequest{
		ChainId:  chainID,
		UniqueId: uniqueID,
		RawBytes: rawBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("grpc SignRawBytes: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("remote signer error: %s", resp.Error.Description)
	}
	g.log.Debug("signed raw bytes via privval gRPC",
		"chain_id", chainID,
		"unique_id", uniqueID,
		"signature_len", len(resp.Signature),
	)
	return resp.Signature, nil
}

// SignVote is not supported by the gRPC privval client (fiber never calls it).
func (g *GRPCClient) SignVote(_ string, _ *cmtproto.Vote) error {
	return fmt.Errorf("SignVote not supported by gRPC privval client")
}

// SignProposal is not supported by the gRPC privval client (fiber never calls it).
func (g *GRPCClient) SignProposal(_ string, _ *cmtproto.Proposal) error {
	return fmt.Errorf("SignProposal not supported by gRPC privval client")
}

// Close closes the underlying gRPC connection.
func (g *GRPCClient) Close() error {
	return g.conn.Close()
}
