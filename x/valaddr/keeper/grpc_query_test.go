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

func TestAllBondedFibreProvidersQuery(t *testing.T) {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(true)

	queryHelper := baseapp.NewQueryServerTestHelper(ctx, testApp.GetEncodingConfig().InterfaceRegistry)
	types.RegisterQueryServer(queryHelper, testApp.ValAddrKeeper)
	queryClient := types.NewQueryClient(queryHelper)

	// Use the consensus address of a real bonded validator: AllBondedFibreProviders
	// only advertises providers whose validator is currently in the active set.
	validators, err := testApp.StakingKeeper.GetBondedValidatorsByPower(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, validators)
	consPubKey, err := validators[0].ConsPubKey()
	require.NoError(t, err)
	bondedConsAddr := sdk.ConsAddress(consPubKey.Address())

	bondedInfo := types.FibreProviderInfo{Host: "bonded.fibre.example.com:7980"}
	require.NoError(t, testApp.ValAddrKeeper.SetFibreProviderInfo(ctx, bondedConsAddr, bondedInfo))

	// A provider for an address that is not a bonded validator must be excluded
	// from the response (it is a stale host).
	staleConsAddr := sdk.ConsAddress("not_a_validator____")
	require.NoError(t, testApp.ValAddrKeeper.SetFibreProviderInfo(ctx, staleConsAddr, types.FibreProviderInfo{Host: "stale.example.com:7980"}))

	resp, err := queryClient.AllBondedFibreProviders(gocontext.Background(), &types.QueryAllBondedFibreProvidersRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Providers, 1)
	require.Equal(t, bondedConsAddr.String(), resp.Providers[0].ValidatorConsensusAddress)
	require.Equal(t, bondedInfo.Host, resp.Providers[0].Info.Host)
}
