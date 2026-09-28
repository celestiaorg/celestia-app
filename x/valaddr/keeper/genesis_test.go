package keeper_test

import (
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/x/valaddr"
	"github.com/celestiaorg/celestia-app/v10/x/valaddr/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	t.Run("default genesis", func(t *testing.T) {
		genesis := valaddr.DefaultGenesisState()
		require.NotNil(t, genesis)
	})

	t.Run("validate genesis", func(t *testing.T) {
		tests := []struct {
			name      string
			genesis   *types.GenesisState
			expectErr bool
		}{
			{
				name:      "valid genesis",
				genesis:   valaddr.DefaultGenesisState(),
				expectErr: false,
			},
			{
				name:      "nil genesis",
				genesis:   nil,
				expectErr: true,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				err := valaddr.ValidateGenesis(tc.genesis)
				if tc.expectErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			})
		}
	})

	t.Run("init genesis", func(t *testing.T) {
		testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
		ctx := testApp.NewContext(true)
		keeper := testApp.ValAddrKeeper

		genesisState := &types.GenesisState{}

		valaddr.InitGenesis(ctx, keeper, genesisState)
	})

	t.Run("export genesis", func(t *testing.T) {
		testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
		ctx := testApp.NewContext(true)
		keeper := testApp.ValAddrKeeper

		exported := valaddr.ExportGenesis(ctx, keeper)

		require.NotNil(t, exported)
	})
}

// TestGenesisRoundTrip exercises the JSON path used by celestia-appd export.
func TestGenesisRoundTrip(t *testing.T) {
	source, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	sourceCtx := source.NewContext(true)
	providers := []types.FibreProvider{
		{ValidatorConsensusAddress: sdk.ConsAddress([]byte("validator-address-02")).String(), Info: types.FibreProviderInfo{Host: "legacy.example.com"}},
		{ValidatorConsensusAddress: sdk.ConsAddress([]byte("validator-address-01")).String(), Info: types.FibreProviderInfo{Host: "provider.example.com:7980"}},
	}
	for _, provider := range providers {
		address, err := sdk.ConsAddressFromBech32(provider.ValidatorConsensusAddress)
		require.NoError(t, err)
		require.NoError(t, source.ValAddrKeeper.SetFibreProviderInfo(sourceCtx, address, provider.Info))
	}
	sourceModule := valaddr.NewAppModule(source.AppCodec(), source.ValAddrKeeper)
	exported := sourceModule.ExportGenesis(sourceCtx, source.AppCodec())
	require.NoError(t, sourceModule.ValidateGenesis(source.AppCodec(), nil, exported))
	var genesis types.GenesisState
	require.NoError(t, source.AppCodec().UnmarshalJSON(exported, &genesis))
	// Export follows store key order, independent of insertion order or staking state.
	require.Equal(t, []types.FibreProvider{providers[1], providers[0]}, genesis.FibreProviders)
	require.Equal(t, exported, sourceModule.ExportGenesis(sourceCtx, source.AppCodec()))
	target, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	targetCtx := target.NewContext(true)
	targetModule := valaddr.NewAppModule(target.AppCodec(), target.ValAddrKeeper)
	targetModule.InitGenesis(targetCtx, target.AppCodec(), exported)
	for _, provider := range providers {
		address, err := sdk.ConsAddressFromBech32(provider.ValidatorConsensusAddress)
		require.NoError(t, err)
		info, found := target.ValAddrKeeper.GetFibreProviderInfo(targetCtx, address)
		require.True(t, found)
		require.Equal(t, provider.Info, info)
	}
	require.JSONEq(t, string(exported), string(targetModule.ExportGenesis(targetCtx, target.AppCodec())))
}

func TestGenesisProviderValidation(t *testing.T) {
	address := sdk.ConsAddress([]byte("validator-address-01")).String()
	for _, tc := range []struct {
		name      string
		addresses []string
		wantErr   string
	}{
		{name: "valid", addresses: []string{address}},
		{name: "empty address", addresses: []string{""}, wantErr: "invalid validator consensus address"},
		{name: "malformed address", addresses: []string{"invalid"}, wantErr: "invalid validator consensus address"},
		{name: "wrong prefix", addresses: []string{sdk.ValAddress([]byte("validator-address-01")).String()}, wantErr: "invalid validator consensus address"},
		{name: "duplicate address", addresses: []string{address, address}, wantErr: "duplicate validator consensus address"},
		{name: "duplicate address with different case", addresses: []string{address, strings.ToUpper(address)}, wantErr: "duplicate validator consensus address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			genesis := valaddr.DefaultGenesisState()
			for _, address := range tc.addresses {
				genesis.FibreProviders = append(genesis.FibreProviders, types.FibreProvider{ValidatorConsensusAddress: address, Info: types.FibreProviderInfo{Host: "legacy.example.com"}})
			}
			err := valaddr.ValidateGenesis(genesis)
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}

func TestLegacyEmptyGenesis(t *testing.T) {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(true)
	module := valaddr.NewAppModule(testApp.AppCodec(), testApp.ValAddrKeeper)
	for _, genesis := range []string{`{}`, `{"fibre_providers":[]}`} {
		require.NoError(t, module.ValidateGenesis(testApp.AppCodec(), nil, []byte(genesis)))
		module.InitGenesis(ctx, testApp.AppCodec(), []byte(genesis))
		require.Empty(t, valaddr.ExportGenesis(ctx, testApp.ValAddrKeeper).FibreProviders)
	}
}
