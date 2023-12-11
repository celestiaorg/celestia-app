package internal

import (
	"context"
	"fmt"
	"time"

	upgradetypes "github.com/celestiaorg/celestia-app/x/upgrade/types"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func QueryVersionTally(grpcEndpoint string, version uint64) error {
	conn, err := grpc.Dial(grpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("did not connect: %v", err)
	}
	defer conn.Close()

	client := upgradetypes.NewQueryClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := client.VersionTally(ctx, &upgradetypes.QueryVersionTallyRequest{Version: version})
	if err != nil {
		return fmt.Errorf("could not query version tally: %v", err)
	}

	fmt.Printf("version: %v\n", version)
	fmt.Printf("total voting power: %v\n", resp.GetTotalVotingPower())
	fmt.Printf("threshold power: %v\n", resp.GetThresholdPower())
	fmt.Printf("voting power: %v\n", resp.GetVotingPower())
	return nil
}
