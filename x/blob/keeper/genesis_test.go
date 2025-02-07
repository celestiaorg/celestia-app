package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/x/blob/types"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
	}

	k, _, ctx := CreateKeeper(t, appconsts.LatestVersion)
	k.InitGenesis(ctx, genesisState)
	got := k.ExportGenesis(ctx)
	require.NotNil(t, got)
	require.Equal(t, types.DefaultParams(), got.Params)
}
