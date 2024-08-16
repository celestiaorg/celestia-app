package types_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"

	"github.com/celestiaorg/celestia-app/x/blobstream/types"

	"github.com/stretchr/testify/require"
)

func TestGenesisStateValidate(t *testing.T) {
	specs := map[string]struct {
		src    *types.GenesisState
		expErr bool
	}{
		"default params": {
			src:    types.DefaultGenesis(),
			expErr: false,
		},
		"empty params": {
			src: &types.GenesisState{
				Params: &types.Params{},
			},
			expErr: true,
		},
		"invalid params: short block time": {
			src: &types.GenesisState{
				Params: &types.Params{
					DataCommitmentWindow: types.MinimumDataCommitmentWindow - 1,
				},
			},
			expErr: true,
		},
		"invalid params: long block time": {
			src: &types.GenesisState{
				Params: &types.Params{
					DataCommitmentWindow: uint64(appconsts.DataCommitmentBlocksLimit + 1),
				},
			},
			expErr: true,
		},
		"valid params: data commitments blocks limit": {
			src: &types.GenesisState{
				Params: &types.Params{
					DataCommitmentWindow: uint64(appconsts.DataCommitmentBlocksLimit),
				},
			},
			expErr: false,
		},
		"valid params: minimum data commitment window": {
			src: &types.GenesisState{
				Params: &types.Params{
					DataCommitmentWindow: types.MinimumDataCommitmentWindow,
				},
			},
			expErr: false,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			err := spec.src.Validate()
			if spec.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
