package keeper_test

import (
	"fmt"
	"testing"

	v1 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v3/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestParamsQuery(t *testing.T) {
	versions := []uint64{v1.Version, v2.Version}
	for _, version := range versions {
		t.Run("AppVersion_"+fmt.Sprint(version), func(t *testing.T) {
			keeper, _, ctx := CreateKeeper(t, v1.Version)
			wctx := sdk.WrapSDKContext(ctx)
			params := types.DefaultParams()
			keeper.SetParams(ctx, params)

			response, err := keeper.Params(wctx, &types.QueryParamsRequest{})
			require.NoError(t, err)
			require.Equal(t, &types.QueryParamsResponse{Params: params}, response)
		})
	}
}
