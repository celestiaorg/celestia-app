//go:build valaddr_wiring

package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/app"
	testutil "github.com/celestiaorg/celestia-app/v9/test/util"
	"github.com/celestiaorg/celestia-app/v9/x/valaddr/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestFibreProviderInfo(t *testing.T) {
	t.Run("set and get", func(t *testing.T) {
		testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
		ctx := testApp.NewContext(true)
		keeper := testApp.ValAddrKeeper

		consAddr := sdk.ConsAddress("validator1")
		info := types.FibreProviderInfo{
			Host: "validator1.fibre.example.com",
		}

		err := keeper.SetFibreProviderInfo(ctx, consAddr, info)
		require.NoError(t, err)

		retrieved, found := keeper.GetFibreProviderInfo(ctx, consAddr)
		require.True(t, found)
		require.Equal(t, info.Host, retrieved.Host)
	})

	t.Run("not found", func(t *testing.T) {
		testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
		ctx := testApp.NewContext(true)
		keeper := testApp.ValAddrKeeper

		consAddr := sdk.ConsAddress("nonexistent")

		_, found := keeper.GetFibreProviderInfo(ctx, consAddr)
		require.False(t, found)
	})

	t.Run("delete", func(t *testing.T) {
		testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
		ctx := testApp.NewContext(true)
		keeper := testApp.ValAddrKeeper

		consAddr := sdk.ConsAddress("validator_to_delete")
		info := types.FibreProviderInfo{
			Host: "validator.fibre.example.com",
		}

		err := keeper.SetFibreProviderInfo(ctx, consAddr, info)
		require.NoError(t, err)

		err = keeper.DeleteFibreProviderInfo(ctx, consAddr)
		require.NoError(t, err)

		_, found := keeper.GetFibreProviderInfo(ctx, consAddr)
		require.False(t, found)
	})

	t.Run("iterate", func(t *testing.T) {
		testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
		ctx := testApp.NewContext(true)
		keeper := testApp.ValAddrKeeper

		providers := []struct {
			consAddr sdk.ConsAddress
			info     types.FibreProviderInfo
		}{
			{sdk.ConsAddress("validator1"), types.FibreProviderInfo{Host: "validator1.fibre.example.com"}},
			{sdk.ConsAddress("validator2"), types.FibreProviderInfo{Host: "validator2.fibre.example.com"}},
			{sdk.ConsAddress("validator3"), types.FibreProviderInfo{Host: "validator3.fibre.example.com"}},
		}

		for _, p := range providers {
			err := keeper.SetFibreProviderInfo(ctx, p.consAddr, p.info)
			require.NoError(t, err)
		}

		count := 0
		err := keeper.IterateFibreProviderInfo(ctx, func(_ sdk.ConsAddress, _ types.FibreProviderInfo) bool {
			count++
			return false
		})
		require.NoError(t, err)
		require.Equal(t, 3, count)
	})
}
