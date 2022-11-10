package keeper_test

import (
	"testing"

	testkeeper "github.com/celestiaorg/celestia-app/testutil/keeper"
	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/stretchr/testify/require"
)

func TestGetParams(t *testing.T) {
	k, ctx := testkeeper.PaymentKeeper(t)
	params := types.DefaultParams()

	k.SetParams(ctx, params)

	require.EqualValues(t, params, k.GetParams(ctx))
	require.EqualValues(t, params.MinSquareSize, k.MinSquareSize(ctx))
	require.EqualValues(t, params.MaxSquareSize, k.MaxSquareSize(ctx))
}
