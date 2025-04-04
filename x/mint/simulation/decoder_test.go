package simulation_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/kv"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/x/mint/simulation"
	"github.com/celestiaorg/celestia-app/v4/x/mint/types"
)

func TestDecodeStore(t *testing.T) {
	cdc := encoding.MakeTestConfig(app.ModuleEncodingRegisters...).Codec
	decoder := simulation.NewDecodeStore(cdc)
	minter := types.NewMinter(math.LegacyOneDec(), math.LegacyNewDec(15), sdk.DefaultBondDenom)
	unixEpoch := time.Unix(0, 0).UTC()
	genesisTime := types.GenesisTime{GenesisTime: &unixEpoch}

	kvPairs := kv.Pairs{
		Pairs: []kv.Pair{
			{Key: types.KeyMinter, Value: cdc.MustMarshal(&minter)},
			{Key: types.KeyGenesisTime, Value: cdc.MustMarshal(&genesisTime)},
			{Key: []byte{0x99}, Value: []byte{0x99}},
		},
	}
	tests := []struct {
		name        string
		expected    string
		expectPanic bool
	}{
		{
			name:        "Minter",
			expected:    fmt.Sprintf("%v\n%v", minter, minter),
			expectPanic: false,
		},
		{
			name:        "GenesisTime",
			expected:    fmt.Sprintf("%v\n%v", genesisTime, genesisTime),
			expectPanic: false,
		},
		{
			name:        "other",
			expected:    "",
			expectPanic: true,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectPanic {
				require.Panics(t, func() { decoder(kvPairs.Pairs[i], kvPairs.Pairs[i]) }, tt.name)
				return
			}
			require.Equal(t, tt.expected, decoder(kvPairs.Pairs[i], kvPairs.Pairs[i]), tt.name)
		})
	}
}
