package sign

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/cometbft/cometbft/crypto"
	privvalproto "github.com/cometbft/cometbft/proto/tendermint/privval"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const signTimeout = 5 * time.Second

// GRPCClient implements [types.PrivValidator] by connecting to a node's
// PrivValidatorAPI gRPC endpoint. This allows fiber to sign payment promises
// via the node's privval without needing an external signer like tmkms.
//
// Because the gRPC service only exposes SignRawBytes (no GetPubKey), the
// public key must be provided at construction time.
type GRPCClient struct {
	conn   *grpc.ClientConn
	client privvalproto.PrivValidatorAPIClient
	pubKey crypto.PubKey
	log    *slog.Logger
}

var (
	_ types.PrivValidator = (*GRPCClient)(nil)
	_ io.Closer           = (*GRPCClient)(nil)
)

// NewGRPCClient dials the given gRPC address with insecure credentials
// (intended for localhost use) and returns a client that delegates
// SignRawBytes to the remote PrivValidatorAPI.
func NewGRPCClient(addr string, pubKey crypto.PubKey, log *slog.Logger) (*GRPCClient, error) {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dialing privval gRPC at %s: %w", addr, err)
	}

	return &GRPCClient{
		conn:   conn,
		client: privvalproto.NewPrivValidatorAPIClient(conn),
		pubKey: pubKey,
		log:    log,
	}, nil
}

// GetPubKey returns the public key provided at construction time.
func (g *GRPCClient) GetPubKey() (crypto.PubKey, error) {
	return g.pubKey, nil
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
