package app_test

import (
	"testing"

	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/celestiaorg/celestia-app/v2/x/minfee"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_testnode(t *testing.T) {
	t.Run("testnode can start a network with default chain ID", func(t *testing.T) {
		testnode.NewNetwork(t, testnode.DefaultConfig())
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
}
