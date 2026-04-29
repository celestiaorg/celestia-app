package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/x/valaddr/types"
	"github.com/stretchr/testify/require"
)

// TestFibreProviderInfoSerialization tests protobuf marshaling/unmarshaling
func TestFibreProviderInfoSerialization(t *testing.T) {
	tests := []struct {
		name string
		info types.FibreProviderInfo
	}{
		{
			name: "DNS hostname",
			info: types.FibreProviderInfo{
				Host: "validator1.fibre.example.com",
			},
		},
		{
			name: "IPv6",
			info: types.FibreProviderInfo{
				Host: "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
			},
		},
		{
			name: "hostname with port",
			info: types.FibreProviderInfo{
				Host: "validator.example.com:8080",
			},
		},
		{
			name: "maximum length IP (89 chars)",
			info: types.FibreProviderInfo{
				Host: "a234567890123456789012345678901234567890123456789012345678901234567890123456789012345678",
			},
		},
		{
			name: "empty host",
			info: types.FibreProviderInfo{
				Host: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bz, err := tt.info.Marshal()
			require.NoError(t, err)
			require.NotNil(t, bz)

			var decoded types.FibreProviderInfo
			err = decoded.Unmarshal(bz)
			require.NoError(t, err)
			require.Equal(t, tt.info.Host, decoded.Host)
		})
	}
}

// TestGenesisStateSerialization tests genesis state serialization
func TestGenesisStateSerialization(t *testing.T) {
	genesisState := types.GenesisState{}

	bz, err := genesisState.Marshal()
	require.NoError(t, err)
	require.NotNil(t, bz)

	var decoded types.GenesisState
	err = decoded.Unmarshal(bz)
	require.NoError(t, err)
}
