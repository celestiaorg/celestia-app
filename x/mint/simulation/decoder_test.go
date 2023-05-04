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
	dec := simulation.NewDecodeStore(cdc)
	unixEpoch := time.Unix(0, 0)
	minter := types.NewMinter(sdk.OneDec(), sdk.NewDec(15), &unixEpoch)

	kvPairs := kv.Pairs{
		Pairs: []kv.Pair{
			{Key: types.MinterKey, Value: cdc.MustMarshal(&minter)},
			{Key: []byte{0x99}, Value: []byte{0x99}},
		},
	}
	tests := []struct {
		name        string
		expectedLog string
	}{
		{"Minter", fmt.Sprintf("%v\n%v", minter, minter)},
		{"other", ""},
	}

	for i, tt := range tests {
		i, tt := i, tt
		t.Run(tt.name, func(t *testing.T) {
			switch i {
			case len(tests) - 1:
				require.Panics(t, func() { dec(kvPairs.Pairs[i], kvPairs.Pairs[i]) }, tt.name)
			default:
				require.Equal(t, tt.expectedLog, dec(kvPairs.Pairs[i], kvPairs.Pairs[i]), tt.name)
			}
		})
	}
}
