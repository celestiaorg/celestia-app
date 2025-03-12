package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

func TestGetParams(t *testing.T) {
	k, _, ctx := CreateKeeper(t, appconsts.LatestVersion)
	params := types.DefaultParams()

	k.SetParams(ctx, params)

	require.EqualValues(t, params, k.GetParams(ctx))
}
