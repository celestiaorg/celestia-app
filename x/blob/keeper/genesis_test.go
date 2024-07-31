package keeper_test

import (
	"testing"

	"github.com/rootulp/celestia-app/x/blob"
	"github.com/rootulp/celestia-app/x/blob/types"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),
	}

	k, _, ctx := CreateKeeper(t)
	blob.InitGenesis(ctx, *k, genesisState)
	got := blob.ExportGenesis(ctx, *k)
	require.NotNil(t, got)
	require.Equal(t, types.DefaultParams(), got.Params)
}
