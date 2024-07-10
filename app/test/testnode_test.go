package app_test

import (
	"context"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

func Test_testnode(t *testing.T) {
	t.Run("testnode can start a network with default chain ID", func(t *testing.T) {
		testnode.NewNetwork(t, testnode.DefaultConfig())
	})
	t.Run("testnode can start with a custom MinGasPrice", func(t *testing.T) {
		// want := "0.000006stake"
		config := testnode.DefaultConfig()
		appConfig := testnode.DefaultAppConfig()
		// appConfig.MinGasPrices = want
		config.WithAppConfig(appConfig)
		_, _, grpcAddr := testnode.NewNetwork(t, config)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		grpcConn := setup(t, ctx, grpcAddr)
		got, err := queryMinimumGasPrice(ctx, grpcConn)
		// got, err := user.QueryMinimumGasPrice(ctx, grpcConn)
		require.NoError(t, err)
		assert.Equal(t, ".002utia", got)
	})
}

func setup(t *testing.T, ctx context.Context, grpcAddr string) *grpc.ClientConn {
	client, err := grpc.NewClient(
		grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	// this ensures we can't start the node without core connection
	client.Connect()
	if !client.WaitForStateChange(ctx, connectivity.Ready) {
		// hits the case when context is canceled
		t.Fatalf("couldn't connect to core endpoint(%s): %v", grpcAddr, ctx.Err())
	}
	return client
}

func queryMinimumGasPrice(ctx context.Context, grpcConn *grpc.ClientConn) (float64, error) {
	cfgRsp, err := nodeservice.NewServiceClient(grpcConn).Config(ctx, &nodeservice.ConfigRequest{})
	if err != nil {
		return 0, err
	}

	localMinCoins, err := sdktypes.ParseDecCoins(cfgRsp.MinimumGasPrice)
	if err != nil {
		return 0, err
	}
	localMinPrice := localMinCoins.AmountOf(app.BondDenom).MustFloat64()
	return localMinPrice, nil
}
