package internal

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	upgradetypes "github.com/celestiaorg/celestia-app/x/upgrade/types"

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
	signedTx, err := os.ReadFile(pathToTransaction)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %v. %v", pathToTransaction, err)
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	decoded, err := encCfg.TxConfig.TxJSONDecoder()(signedTx)
	if err != nil {
		return nil, fmt.Errorf("failed to decode transaction: %v", err)
	}

	txBytes, err := encCfg.TxConfig.TxEncoder()(decoded)
	if err != nil {
		return nil, fmt.Errorf("failed to encode transaction: %v", err)
	}

	client := tx.NewServiceClient(conn)
	res, err := client.BroadcastTx(context.Background(), &tx.BroadcastTxRequest{
		Mode:    tx.BroadcastMode_BROADCAST_MODE_BLOCK,
		TxBytes: txBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to broadcast transaction: %v", err)
	}
	return res.GetTxResponse(), nil
}

func IsUpgradeable(response *upgradetypes.QueryVersionTallyResponse) bool {
    if response == nil {
        return false
    }
    return response.GetVotingPower() > response.ThresholdPower
}
