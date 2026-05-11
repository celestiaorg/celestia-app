package v3

import (
	"context"

	"github.com/celestiaorg/celestia-app/v9/app/grpc/tx"
	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	"google.golang.org/grpc"
)

// txBroadcaster handles the network-facing side of the pipeline: sending
// signed tx bytes to a node and polling for inclusion status.
type txBroadcaster interface {
	Submit(ctx context.Context, txBytes []byte) error
	Status(ctx context.Context, txHash string) (*tx.TxStatusResponse, error)
}

// grpcTxBroadcaster is the production txBroadcaster, talking to a single
// gRPC connection.
type grpcTxBroadcaster struct {
	v1   *user.TxClient
	conn *grpc.ClientConn
	tx   tx.TxClient
}

func newGRPCTxBroadcaster(v1 *user.TxClient, conn *grpc.ClientConn) *grpcTxBroadcaster {
	return &grpcTxBroadcaster{
		v1:   v1,
		conn: conn,
		tx:   tx.NewTxClient(conn),
	}
}

func (b *grpcTxBroadcaster) Submit(ctx context.Context, txBytes []byte) error {
	_, err := b.v1.SendTxToConnection(ctx, b.conn, txBytes)
	return err
}

func (b *grpcTxBroadcaster) Status(ctx context.Context, txHash string) (*tx.TxStatusResponse, error) {
	return b.tx.TxStatus(ctx, &tx.TxStatusRequest{TxId: txHash})
}
