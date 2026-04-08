//go:build valaddr_wiring

package keeper_test

import (
	gocontext "context"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/app"
	testutil "github.com/celestiaorg/celestia-app/v9/test/util"
	"github.com/celestiaorg/celestia-app/v9/x/valaddr/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestFibreProviderInfoQuery(t *testing.T) {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(true)

	queryHelper := baseapp.NewQueryServerTestHelper(ctx, testApp.GetEncodingConfig().InterfaceRegistry)
	types.RegisterQueryServer(queryHelper, testApp.ValAddrKeeper)
	queryClient := types.NewQueryClient(queryHelper)

	t.Run("found", func(t *testing.T) {
		consAddr := sdk.ConsAddress("validator1")
		info := types.FibreProviderInfo{
			Host: "validator1.fibre.example.com",
		}

		err := testApp.ValAddrKeeper.SetFibreProviderInfo(ctx, consAddr, info)
		require.NoError(t, err)

		resp, err := queryClient.FibreProviderInfo(gocontext.Background(), &types.QueryFibreProviderInfoRequest{
			ValidatorConsensusAddress: consAddr.String(),
		})
		require.NoError(t, err)
		require.True(t, resp.Found)
		require.Equal(t, info.Host, resp.Info.Host)
	})

	t.Run("not found", func(t *testing.T) {
		consAddr := sdk.ConsAddress("nonexistent")

		resp, err := queryClient.FibreProviderInfo(gocontext.Background(), &types.QueryFibreProviderInfoRequest{
			ValidatorConsensusAddress: consAddr.String(),
		})
		require.NoError(t, err)
		require.False(t, resp.Found)
	})
}

func TestAllFibreProvidersQuery(t *testing.T) {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(true)

	queryHelper := baseapp.NewQueryServerTestHelper(ctx, testApp.GetEncodingConfig().InterfaceRegistry)
	types.RegisterQueryServer(queryHelper, testApp.ValAddrKeeper)
	queryClient := types.NewQueryClient(queryHelper)

	consAddr1 := sdk.ConsAddress("validator1")
	info1 := types.FibreProviderInfo{
		Host: "validator1.fibre.example.com",
	}
	err := testApp.ValAddrKeeper.SetFibreProviderInfo(ctx, consAddr1, info1)
	require.NoError(t, err)

	consAddr2 := sdk.ConsAddress("validator2")
	info2 := types.FibreProviderInfo{
		Host: "validator2.fibre.example.com",
	}
	err = testApp.ValAddrKeeper.SetFibreProviderInfo(ctx, consAddr2, info2)
	require.NoError(t, err)

	resp, err := queryClient.AllFibreProviders(gocontext.Background(), &types.QueryAllFibreProvidersRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Providers, 2)
}
