package user

import (
	"context"
	"fmt"
	"strings"

	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc"

	"github.com/celestiaorg/celestia-app/v4/app/params"
	minfeetypes "github.com/celestiaorg/celestia-app/v4/x/minfee/types"
)

// QueryMinimumGasPrice queries both the nodes local and network wide
// minimum gas prices, returning the maximum of the two.
func QueryMinimumGasPrice(ctx context.Context, grpcConn *grpc.ClientConn) (float64, error) {
	cfgRsp, err := nodeservice.NewServiceClient(grpcConn).Config(ctx, &nodeservice.ConfigRequest{})
	if err != nil {
		return 0, err
	}

	localMinCoins, err := sdk.ParseDecCoins(cfgRsp.MinimumGasPrice)
	if err != nil {
		return 0, err
	}
	localMinPrice := localMinCoins.AmountOf(params.BondDenom).MustFloat64()

	networkMinPrice, err := QueryNetworkMinGasPrice(ctx, grpcConn)
	if err != nil {
		// check if the network version supports a global min gas
		// price using a regex check. If not (i.e. v1) use the
		// local price only
		if strings.Contains(err.Error(), "unknown subspace: minfee") {
			return localMinPrice, nil
		}
		return 0, err
	}

	// return the highest value of the two
	return max(localMinPrice, networkMinPrice), nil
}

func QueryNetworkMinGasPrice(ctx context.Context, grpcConn *grpc.ClientConn) (float64, error) {
	minfeeClient := minfeetypes.NewQueryClient(grpcConn)
	// Query the network minimum gas price directly from the minfee module
	paramResponse, err := minfeeClient.NetworkMinGasPrice(ctx, &minfeetypes.QueryNetworkMinGasPrice{})
	if err != nil {
		return 0, fmt.Errorf("querying minfee module: %w", err)
	}

	// Convert the network min gas price from LegacyDec to float64
	networkMinPrice := paramResponse.NetworkMinGasPrice.MustFloat64()
	return networkMinPrice, nil
}
