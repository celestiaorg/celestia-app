package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v3/x/blobstream"

	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/x/blobstream/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDataCommitmentForHeight(t *testing.T) {
	input, sdkCtx := testutil.SetupFiveValChain(t)
	k := input.BlobstreamKeeper

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
			EndBlock:   window * (i + 1),
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
			name:          "height within range of latest data commitment",
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
			expectedDCC:   dcs[2],
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
			expectedError: "Latest height 4000 < 40000: no data commitment has been generated for the provided height",
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

func TestLatestDataCommitment(t *testing.T) {
	input, sdkCtx := testutil.SetupFiveValChain(t)
	k := input.BlobstreamKeeper

	initialValset, err := k.GetCurrentValset(sdkCtx)
	require.NoError(t, err)

	// setting initial valset
	err = k.SetAttestationRequest(sdkCtx, &initialValset)
	require.NoError(t, err)

	// trying to get the latest data commitment
	dcc, err := k.GetLatestDataCommitment(sdkCtx)
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

	// getting the latest data commitment
	dcc, err = k.GetLatestDataCommitment(sdkCtx)
	assert.NoError(t, err)
	assert.Equal(t, dcs[3], dcc)
}

func TestCheckingLatestAttestationNonceInDataCommitments(t *testing.T) {
	input := testutil.CreateTestEnvWithoutBlobstreamKeysInit(t)
	k := input.BlobstreamKeeper

	tests := []struct {
		name          string
		requestFunc   func() error
		expectedError error
	}{
		{
			name: "check latest nonce before getting current data commitment",
			requestFunc: func() error {
				_, err := k.NextDataCommitment(input.Context)
				return err
			},
			expectedError: types.ErrLatestAttestationNonceStillNotInitialized,
		},
		{
			name: "check latest nonce before getting data commitment for height",
			requestFunc: func() error {
				_, err := k.GetDataCommitmentForHeight(input.Context, 1)
				return err
			},
			expectedError: types.ErrLatestAttestationNonceStillNotInitialized,
		},
		{
			name: "check latest nonce before getting latest data commitment",
			requestFunc: func() error {
				_, err := k.GetLatestDataCommitment(input.Context)
				return err
			},
			expectedError: types.ErrLatestAttestationNonceStillNotInitialized,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.requestFunc()
			assert.ErrorIs(t, err, tt.expectedError)
		})
	}
}

func TestCheckingEarliestAvailableAttestationNonceInDataCommitments(t *testing.T) {
	input := testutil.CreateTestEnvWithoutBlobstreamKeysInit(t)
	k := input.BlobstreamKeeper

	// init the latest attestation nonce
	input.BlobstreamKeeper.SetLatestAttestationNonce(input.Context, blobstream.InitialLatestAttestationNonce)

	tests := []struct {
		name          string
		requestFunc   func() error
		expectedError error
	}{
		{
			name: "check earliest available attestation nonce before getting current data commitment",
			requestFunc: func() error {
				_, err := k.NextDataCommitment(input.Context)
				return err
			},
			expectedError: types.ErrEarliestAvailableNonceStillNotInitialized,
		},
		{
			name: "check earliest available attestation nonce before getting data commitment for height",
			requestFunc: func() error {
				_, err := k.GetDataCommitmentForHeight(input.Context, 1)
				return err
			},
			expectedError: types.ErrEarliestAvailableNonceStillNotInitialized,
		},
		{
			name: "check earliest available attestation nonce before getting latest data commitment",
			requestFunc: func() error {
				_, err := k.GetLatestDataCommitment(input.Context)
				return err
			},
			expectedError: types.ErrEarliestAvailableNonceStillNotInitialized,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.requestFunc()
			assert.ErrorIs(t, err, tt.expectedError)
		})
	}
}
