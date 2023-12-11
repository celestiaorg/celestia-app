package internal

import (
	"context"
	"fmt"
	"time"

	upgradetypes "github.com/celestiaorg/celestia-app/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func QueryVersionTally(grpcEndpoint string, version uint64) (*upgradetypes.QueryVersionTallyResponse, error) {
	conn, err := grpc.Dial(grpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("did not connect: %v", err)
	}
	defer conn.Close()

	client := upgradetypes.NewQueryClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := client.VersionTally(ctx, &upgradetypes.QueryVersionTallyRequest{Version: version})
	if err != nil {
		return nil, fmt.Errorf("could not query version tally: %v", err)
	}
	return resp, nil
}

func SubmitTryUpgrade(grpcEndpoint string, addr sdk.AccAddress) (*upgradetypes.MsgTryUpgradeResponse, error) {
	conn, err := grpc.Dial(grpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("did not connect: %v", err)
	}
	defer conn.Close()

	client := upgradetypes.NewMsgClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := client.TryUpgrade(ctx, upgradetypes.NewMsgTryUpgrade(addr))
	if err != nil {
		return nil, fmt.Errorf("could not submit try upgrade: %v", err)
	}
	return resp, nil
}

func IsUpgradeable(response *upgradetypes.QueryVersionTallyResponse) bool {
	return response.GetVotingPower() > response.ThresholdPower
}
