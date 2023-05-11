package paramfilter

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/square"

	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

func Test_validateBlockParams(t *testing.T) {
	maxBlockBytes := square.EstimateMaxBlockBytes(appconsts.MaxSquareSize)
	testCases := []struct {
		arg       interface{}
		expectErr bool
	}{
		{nil, true},
		{&abci.BlockParams{}, true},
		{abci.BlockParams{}, true},
		{abci.BlockParams{MaxBytes: -1, MaxGas: -1}, true},
		{abci.BlockParams{MaxBytes: 2000000, MaxGas: -5}, true},
		{abci.BlockParams{MaxBytes: maxBlockBytes, MaxGas: -1}, false},
		{abci.BlockParams{MaxBytes: maxBlockBytes + 1, MaxGas: -1}, true},
	}

	validator := newBlockParamsValidator(appconsts.MaxSquareSize)

	for _, tc := range testCases {
		require.Equal(t, tc.expectErr, validator(tc.arg) != nil)
	}
}
