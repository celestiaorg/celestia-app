package types_test

import (
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestDefaultGenesisState(t *testing.T) {
	genState := types.DefaultGenesisState()
	require.NotNil(t, genState)
	require.Empty(t, genState.Providers)
	
	// Validate that default genesis is valid
	err := types.ValidateGenesis(*genState)
	require.NoError(t, err)
}

func TestNewGenesisState(t *testing.T) {
	validatorAddr := sdk.ValAddress("cosmosvaloper1xyerxdp4xcmnswfsxyerxdp4xcmnswfs0t7p50")
	
	providers := []types.GenesisProvider{
		{
			ValidatorAddress: validatorAddr.String(),
			Info: types.FibreProviderInfo{
				IpAddress: "192.168.1.1",
			},
		},
	}
	
	genState := types.NewGenesisState(providers)
	require.NotNil(t, genState)
	require.Len(t, genState.Providers, 1)
	require.Equal(t, validatorAddr.String(), genState.Providers[0].ValidatorAddress)
	require.Equal(t, "192.168.1.1", genState.Providers[0].Info.IpAddress)
}

func TestValidateGenesis(t *testing.T) {
	validatorAddr1 := sdk.ValAddress("cosmosvaloper1xyerxdp4xcmnswfsxyerxdp4xcmnswfs0t7p50")
	validatorAddr2 := sdk.ValAddress("cosmosvaloper1xyerxdp4xcmnswfsxyerxdp4xcmnswfs123456")
	
	tests := []struct {
		name        string
		genState    types.GenesisState
		expectedErr string
	}{
		{
			name: "valid genesis state",
			genState: types.GenesisState{
				Providers: []types.GenesisProvider{
					{
						ValidatorAddress: validatorAddr1.String(),
						Info: types.FibreProviderInfo{
							IpAddress: "192.168.1.1",
						},
					},
					{
						ValidatorAddress: validatorAddr2.String(),
						Info: types.FibreProviderInfo{
							IpAddress: "192.168.1.2",
						},
					},
				},
			},
			expectedErr: "",
		},
		{
			name: "valid genesis state with IPv6",
			genState: types.GenesisState{
				Providers: []types.GenesisProvider{
					{
						ValidatorAddress: validatorAddr1.String(),
						Info: types.FibreProviderInfo{
							IpAddress: "2001:db8::1",
						},
					},
				},
			},
			expectedErr: "",
		},
		{
			name: "empty genesis state",
			genState: types.GenesisState{
				Providers: []types.GenesisProvider{},
			},
			expectedErr: "",
		},
		{
			name: "invalid validator address",
			genState: types.GenesisState{
				Providers: []types.GenesisProvider{
					{
						ValidatorAddress: "invalid-address",
						Info: types.FibreProviderInfo{
							IpAddress: "192.168.1.1",
						},
					},
				},
			},
			expectedErr: "invalid validator address",
		},
		{
			name: "empty validator address",
			genState: types.GenesisState{
				Providers: []types.GenesisProvider{
					{
						ValidatorAddress: "",
						Info: types.FibreProviderInfo{
							IpAddress: "192.168.1.1",
						},
					},
				},
			},
			expectedErr: "invalid validator address",
		},
		{
			name: "duplicate validator address",
			genState: types.GenesisState{
				Providers: []types.GenesisProvider{
					{
						ValidatorAddress: validatorAddr1.String(),
						Info: types.FibreProviderInfo{
							IpAddress: "192.168.1.1",
						},
					},
					{
						ValidatorAddress: validatorAddr1.String(),
						Info: types.FibreProviderInfo{
							IpAddress: "192.168.1.2",
						},
					},
				},
			},
			expectedErr: "invalid validator address",
		},
		{
			name: "empty IP address",
			genState: types.GenesisState{
				Providers: []types.GenesisProvider{
					{
						ValidatorAddress: validatorAddr1.String(),
						Info: types.FibreProviderInfo{
							IpAddress: "",
						},
					},
				},
			},
			expectedErr: "IP address cannot be empty",
		},
		{
			name: "IP address too long",
			genState: types.GenesisState{
				Providers: []types.GenesisProvider{
					{
						ValidatorAddress: validatorAddr1.String(),
						Info: types.FibreProviderInfo{
							IpAddress: strings.Repeat("a", types.MaxIPAddressLength+1),
						},
					},
				},
			},
			expectedErr: "IP address too long",
		},
		{
			name: "maximum length IP address",
			genState: types.GenesisState{
				Providers: []types.GenesisProvider{
					{
						ValidatorAddress: validatorAddr1.String(),
						Info: types.FibreProviderInfo{
							IpAddress: strings.Repeat("a", types.MaxIPAddressLength),
						},
					},
				},
			},
			expectedErr: "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := types.ValidateGenesis(tt.genState)
			if tt.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedErr)
			}
		})
	}
}

func TestValidateGenesis_LargeState(t *testing.T) {
	// Test with a large number of providers to ensure performance is acceptable
	providers := make([]types.GenesisProvider, 1000)
	for i := 0; i < 1000; i++ {
		valAddr := sdk.ValAddress([]byte("validator" + string(rune(i))))
		providers[i] = types.GenesisProvider{
			ValidatorAddress: valAddr.String(),
			Info: types.FibreProviderInfo{
				IpAddress: "192.168.1.1",
			},
		}
	}
	
	genState := types.GenesisState{
		Providers: providers,
	}
	
	err := types.ValidateGenesis(genState)
	require.NoError(t, err)
}