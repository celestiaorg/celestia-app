package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/x/blob/types"
	"github.com/stretchr/testify/require"
)

func TestGetParams(t *testing.T) {
	k, _, ctx := CreateKeeper(t, appconsts.Version)
	params := types.DefaultParams()

	k.SetParams(ctx, params)

	require.EqualValues(t, params, k.GetParams(ctx))
}

// TestGetParamsIgnoresLegacySubspace checks that GetParams reads only the blob
// store. The v11 upgrade and InitGenesis guarantee the store has params.
func TestGetParamsIgnoresLegacySubspace(t *testing.T) {
	k, _, ctx := CreateKeeper(t, appconsts.Version)
	k.SetParamsLegacy(ctx, types.NewParams(types.DefaultGasPerBlobByte, 64))
	ctx.KVStore(k.StoreKey()).Delete([]byte(types.ParamsKey))

	require.Equal(t, types.Params{}, k.GetParams(ctx))
}
