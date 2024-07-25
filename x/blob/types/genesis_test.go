package types_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/stretchr/testify/require"
)

func TestGenesisState_Validate(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		genState *types.GenesisState
		valid    bool
	}{
		{
			desc:     "default is valid",
			genState: types.DefaultGenesis(),
			valid:    true,
		},
		{
			desc: "valid genesis state",
			genState: &types.GenesisState{
				Params: types.Params{
					GasPerBlobByte:   20,
					GovMaxSquareSize: uint64(appconsts.DefaultSquareSizeUpperBound),
				},
			},
			valid: true,
		},
		{
			desc: "invalid genesis state because GovMaxSquareSize",
			genState: &types.GenesisState{
				Params: types.Params{
					GasPerBlobByte:   20,
					GovMaxSquareSize: uint64(appconsts.DefaultSquareSizeUpperBound + 1),
				},
			},
			valid: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.genState.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
