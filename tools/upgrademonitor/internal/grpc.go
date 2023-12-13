package internal

import (
	"context"
	"fmt"
	"time"

	upgradetypes "github.com/celestiaorg/celestia-app/x/upgrade/types"

	"github.com/cosmos/cosmos-sdk/client/grpc/tmservice"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	"google.golang.org/grpc"
)

func QueryVersionTally(conn *grpc.ClientConn, version uint64) (*upgradetypes.QueryVersionTallyResponse, error) {
	client := upgradetypes.NewQueryClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := client.VersionTally(ctx, &upgradetypes.QueryVersionTallyRequest{Version: version})
	if err != nil {
		return nil, fmt.Errorf("could not query version tally: %v", err)
	}
	return resp, nil
}

func Publish(conn *grpc.ClientConn, pathToTransaction string) (*types.TxResponse, error) {
	client := tx.NewServiceClient(conn)
	txBytes, err := getTxBytes(pathToTransaction)
	if err != nil {
		return nil, err
	}
	res, err := client.BroadcastTx(context.Background(), &tx.BroadcastTxRequest{
		Mode:    tx.BroadcastMode_BROADCAST_MODE_BLOCK,
		TxBytes: txBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to broadcast transaction: %v", err)
	}
	return res.GetTxResponse(), nil
}

func QueryCurrentVersion(conn *grpc.ClientConn) (uint64, error) {
	client := tmservice.NewServiceClient(conn)
	resp, err := client.GetLatestBlock(context.Background(), &tmservice.GetLatestBlockRequest{})
	if err != nil {
		return 0, err
	}
	return resp.GetBlock().GetHeader().Version.App, nil
}
