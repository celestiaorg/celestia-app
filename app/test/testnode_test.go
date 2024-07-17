package app_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/types"
	"google.golang.org/grpc"
)

func Test_testnode(t *testing.T) {
	t.Run("testnode can start a network with default chain ID", func(t *testing.T) {
		testnode.NewNetwork(t, testnode.DefaultConfig())
	})
	t.Run("testnode can start with a custom MinGasPrice", func(t *testing.T) {
		wantMinGasPrice := float64(0.003)
		appConfig := testnode.DefaultAppConfig()
		appConfig.MinGasPrices = fmt.Sprintf("%v%s", wantMinGasPrice, app.BondDenom)
		config := testnode.DefaultConfig().WithAppConfig(appConfig)
		cctx, _, _ := testnode.NewNetwork(t, config)

		got, err := queryMinimumGasPrice(cctx.GoContext(), cctx.GRPCClient)
		require.NoError(t, err)
		assert.Equal(t, wantMinGasPrice, got)
	})
	t.Run("testnode can query CometBFT events", func(t *testing.T) {
		cctx, _, _ := testnode.NewNetwork(t, testnode.DefaultConfig())
		client := cctx.Client

		newBlockSubscriber := "NewBlock/Events"
		newDataSignedBlockQuery := types.QueryForEvent(types.EventSignedBlock).String()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		t.Cleanup(cancel)

		eventChan, err := client.Subscribe(ctx, newBlockSubscriber, newDataSignedBlockQuery)
		require.NoError(t, err)

		for i := 1; i <= 3; i++ {
			select {
			case evt := <-eventChan:
				h := evt.Data.(types.EventDataSignedBlock).Header.Height
				block, err := client.Block(ctx, &h)
				require.NoError(t, err)
				require.GreaterOrEqual(t, block.Block.Height, int64(i))
			case <-ctx.Done():
				require.NoError(t, ctx.Err())
			}
		}

		// unsubscribe to event channel
		require.NoError(t, client.Unsubscribe(ctx, newBlockSubscriber, newDataSignedBlockQuery))
	})
}

func queryMinimumGasPrice(ctx context.Context, grpcConn *grpc.ClientConn) (float64, error) {
	resp, err := nodeservice.NewServiceClient(grpcConn).Config(ctx, &nodeservice.ConfigRequest{})
	if err != nil {
		return 0, err
	}

	minGasCoins, err := sdktypes.ParseDecCoins(resp.MinimumGasPrice)
	if err != nil {
		return 0, err
	}

	minGasPrice := minGasCoins.AmountOf(app.BondDenom).MustFloat64()
	return minGasPrice, nil
}
