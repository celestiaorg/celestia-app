package keeper_test

import (
	"testing"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDataCommitmentForHeight(t *testing.T) {
	input, sdkCtx := testutil.SetupFiveValChain(t)
	k := input.QgbKeeper

	initialValset, err := k.GetCurrentValset(sdkCtx)
	require.NoError(t, err)

	// setting initial valset
	err = k.SetAttestationRequest(sdkCtx, &initialValset)
	require.NoError(t, err)

	// getting the data commitment window
	window := k.GetDataCommitmentWindowParam(sdkCtx)

	dcs := make([]types.DataCommitment, 10)

	// setting some data commitments to be tested against
	for i := uint64(0); i < uint64(len(dcs)); i++ {
		dcs[i] = types.DataCommitment{
			Nonce:      i + 2, // because nonces start at 1, and we already set that one for the initial valset
			BeginBlock: window * i,
			EndBlock:   window*(i+1) - 1,
		}
		err = k.SetAttestationRequest(sdkCtx, &dcs[i])
		require.NoError(t, err)
	}

	tests := []struct {
		name          string
		height        uint64
		expectedDCC   types.DataCommitment
		expectError   bool
		expectedError string
	}{
		{
			name:          "height within range of first data commitment",
			height:        (dcs[0].EndBlock - dcs[0].BeginBlock) / 2,
			expectedDCC:   dcs[0],
			expectError:   false,
			expectedError: "",
		},
		{
			name:          "height within range of second data commitment",
			height:        dcs[1].EndBlock - window/2,
			expectedDCC:   dcs[1],
			expectError:   false,
			expectedError: "",
		},
		{
			name:          "height within range of last data commitment",
			height:        dcs[len(dcs)-1].EndBlock - window/2,
			expectedDCC:   dcs[len(dcs)-1],
			expectError:   false,
			expectedError: "",
		},
		{
			name:          "height is begin block",
			height:        dcs[1].BeginBlock,
			expectedDCC:   dcs[1],
			expectError:   false,
			expectedError: "",
		},
		{
			name:          "height is end block",
			height:        dcs[1].EndBlock,
			expectedDCC:   dcs[1],
			expectError:   false,
			expectedError: "",
		},
		{
			name:          "height 0",
			height:        0,
			expectedDCC:   dcs[0],
			expectError:   false,
			expectedError: "",
		},
		{
			name:          "height still not committed to",
			height:        window * 100,
			expectedDCC:   types.DataCommitment{},
			expectError:   true,
			expectedError: "Last height 3999 < 40000: no data commitment has been generated for the provided height",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dcc, err := k.GetDataCommitmentForHeight(sdkCtx, tt.height)
			if tt.expectError {
				require.Error(t, err)
				assert.Equal(t, tt.expectedError, err.Error())
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedDCC, dcc)
			}
		})
	}
}

func TestLastDataCommitment(t *testing.T) {
	input, sdkCtx := testutil.SetupFiveValChain(t)
	k := input.QgbKeeper

	initialValset, err := k.GetCurrentValset(sdkCtx)
	require.NoError(t, err)

	// setting initial valset
	err = k.SetAttestationRequest(sdkCtx, &initialValset)
	require.NoError(t, err)

	// trying to get the last data commitment
	dcc, err := k.GetLastDataCommitment(sdkCtx)
	assert.Error(t, err)
	assert.Equal(t, dcc, types.DataCommitment{})

	// getting the data commitment window
	window := k.GetDataCommitmentWindowParam(sdkCtx)

	dcs := make([]types.DataCommitment, 4)

	// setting some data commitments to be tested against
	for i := uint64(0); i < uint64(len(dcs)); i++ {
		dcs[i] = types.DataCommitment{
			Nonce:      i + 2, // because nonces start at 1, and we already set that one for the initial valset
			BeginBlock: window * i,
			EndBlock:   window*(i+1) - 1,
		}
		err = k.SetAttestationRequest(sdkCtx, &dcs[i])
		require.NoError(t, err)
	}

	// getting the last data commitment
	dcc, err = k.GetLastDataCommitment(sdkCtx)
	assert.NoError(t, err)
	assert.Equal(t, dcs[3], dcc)
}
