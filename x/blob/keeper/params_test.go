package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/x/blob/types"
	"github.com/stretchr/testify/require"
)

func TestGetParams(t *testing.T) {
	k, _, ctx := CreateKeeper(t)
	params := types.DefaultParams()

	k.SetParams(ctx, params)

	require.EqualValues(t, params, k.GetParams(ctx))
	require.EqualValues(t, params.GasPerBlobByte, k.GasPerBlobByte(ctx))
}
