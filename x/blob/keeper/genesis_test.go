package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/x/blob/types"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
	}

	k, _, ctx := CreateKeeper(t, appconsts.Version)
	err := k.InitGenesis(ctx, genesisState)
	require.NoError(t, err)
	got := k.ExportGenesis(ctx)
	require.NotNil(t, got)
	require.Equal(t, types.DefaultParams(), got.Params)
}
