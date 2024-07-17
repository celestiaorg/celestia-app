package app_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/celestiaorg/celestia-app/v2/x/minfee"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/types"
)

func Test_testnode(t *testing.T) {
	t.Run("testnode can start a network with default chain ID", func(t *testing.T) {
		testnode.NewNetwork(t, testnode.DefaultConfig())
	})
	t.Run("testnode can start a network with a custom chain ID", func(t *testing.T) {
		chainID := "custom-chain-id"
		config := testnode.DefaultConfig().WithChainID(chainID)
		testnode.NewNetwork(t, config)
	})
	t.Run("testnode can query network min gas price", func(t *testing.T) {
		config := testnode.DefaultConfig()
		cctx, _, _ := testnode.NewNetwork(t, config)

		queryClient := minfee.NewQueryClient(cctx.GRPCClient)
		resp, err := queryClient.NetworkMinGasPrice(cctx.GoContext(), &minfee.QueryNetworkMinGasPrice{})
		require.NoError(t, err)
		got, err := resp.NetworkMinGasPrice.Float64()
		require.NoError(t, err)
		assert.Equal(t, v2.NetworkMinGasPrice, got)
	})
	t.Run("testnode can query local min gas price", func(t *testing.T) {
		config := testnode.DefaultConfig()
		cctx, _, _ := testnode.NewNetwork(t, config)

		serviceClient := nodeservice.NewServiceClient(cctx.GRPCClient)
		resp, err := serviceClient.Config(cctx.GoContext(), &nodeservice.ConfigRequest{})
		require.NoError(t, err)
		want := "0.002000000000000000utia"
		assert.Equal(t, want, resp.MinimumGasPrice)
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
				fmt.Printf("Block height: %d\n", h)
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
