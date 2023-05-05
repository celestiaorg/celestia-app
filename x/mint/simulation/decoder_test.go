package simulation_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/x/mint/simulation"
	"github.com/celestiaorg/celestia-app/x/mint/types"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/kv"
)

func TestDecodeStore(t *testing.T) {
	cdc := simapp.MakeTestEncodingConfig().Codec
	decoder := simulation.NewDecodeStore(cdc)
	unixEpoch := time.Unix(0, 0).UTC()
	minter := types.NewMinter(sdk.OneDec(), sdk.NewDec(15), &unixEpoch, sdk.DefaultBondDenom)

	kvPairs := kv.Pairs{
		Pairs: []kv.Pair{
			{Key: types.MintKey, Value: cdc.MustMarshal(&minter)},
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
			name:        "other",
			expected:    "",
			expectPanic: true,
		},
	}

	for i, tt := range tests {
		i, tt := i, tt
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectPanic {
				require.Panics(t, func() { decoder(kvPairs.Pairs[i], kvPairs.Pairs[i]) }, tt.name)
				return
			}
			require.Equal(t, tt.expected, decoder(kvPairs.Pairs[i], kvPairs.Pairs[i]), tt.name)
		})
	}
}
