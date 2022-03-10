package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenesisStateValidate(t *testing.T) {
	specs := map[string]struct {
		src    *GenesisState
		expErr bool
	}{
		"default params": {src: DefaultGenesis(), expErr: false},
		"empty params": {src: &GenesisState{
			Params: &Params{
				DataCommitmentWindow: 0,
			},
		}, expErr: true},
		"invalid params: short block time": {src: &GenesisState{
			Params: &Params{
				DataCommitmentWindow: 10,
			},
		}, expErr: true},
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
