package keeper_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/celestiaorg/celestia-app/v2/x/blobstream"

	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/x/blobstream/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test that valset creation produces the expected normalized power values.
func TestCurrentValsetNormalization(t *testing.T) {
	// Setup the overflow test
	maxPower64 := make([]uint64, 64)             // users with max power (approx 2^63)
	expPower64 := make([]uint64, 64)             // expected scaled powers
	evmAddrs64 := make([]gethcommon.Address, 64) // need 64 eth addresses for this test
	for i := 0; i < 64; i++ {
		maxPower64[i] = uint64(9223372036854775807)
		expPower64[i] = 67108864 // 2^32 split amongst 64 validators
		evmAddrs64[i] = gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(i + 1)}, 20))
	}

	// any lower than this and a validator won't be created
	const minStake = 1000000

	specs := map[string]struct {
		srcPowers []uint64
		expPowers []uint64
	}{
		"one": {
			srcPowers: []uint64{minStake},
			expPowers: []uint64{4294967296},
		},
		"two": {
			srcPowers: []uint64{minStake * 99, minStake * 1},
			expPowers: []uint64{4252017623, 42949672},
		},
		"four equal": {
			srcPowers: []uint64{minStake, minStake, minStake, minStake},
			expPowers: []uint64{1073741824, 1073741824, 1073741824, 1073741824},
		},
		"four equal max power": {
			srcPowers: []uint64{4294967296, 4294967296, 4294967296, 4294967296},
			expPowers: []uint64{1073741824, 1073741824, 1073741824, 1073741824},
		},
		"overflow": {
			srcPowers: maxPower64,
			expPowers: expPower64,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			input, ctx := testutil.SetupTestChain(t, spec.srcPowers)
			r, err := input.BlobstreamKeeper.GetCurrentValset(ctx)
			require.NoError(t, err)
			rMembers, err := types.BridgeValidators(r.Members).ToInternal()
			require.NoError(t, err)
			assert.Equal(t, spec.expPowers, rMembers.GetPowers())
		})
	}
}

func TestCheckingEarliestAvailableAttestationNonceInValsets(t *testing.T) {
	input := testutil.CreateTestEnvWithoutBlobstreamKeysInit(t)
	k := input.BlobstreamKeeper
	// create a validator to have a realistic scenario
	testutil.CreateValidator(
		t,
		input,
		testutil.AccAddrs[0],
		testutil.AccPubKeys[0],
		0,
		testutil.ValAddrs[0],
		testutil.ConsPubKeys[0],
		testutil.StakingAmount,
	)
	// Run the staking endblocker to ensure valset is correct in state
	staking.EndBlocker(input.Context, input.StakingKeeper)

	// init the latest attestation nonce
	input.BlobstreamKeeper.SetLatestAttestationNonce(input.Context, blobstream.InitialLatestAttestationNonce)

	tests := []struct {
		name          string
		requestFunc   func() error
		expectedError error
	}{
		{
			name: "check earliest available nonce before getting the latest valset",
			requestFunc: func() error {
				_, err := k.GetLatestValset(input.Context)
				return err
			},
			expectedError: types.ErrEarliestAvailableNonceStillNotInitialized,
		},
		{
			name: "check earliest available nonce before getting latest valset before nonce",
			requestFunc: func() error {
				_, err := k.GetLatestValsetBeforeNonce(input.Context, 1)
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

func TestCheckingAttestationNonceInValsets(t *testing.T) {
	input := testutil.CreateTestEnvWithoutBlobstreamKeysInit(t)
	k := input.BlobstreamKeeper
	// create a validator to have a  realistic scenario
	testutil.CreateValidator(
		t,
		input,
		testutil.AccAddrs[0],
		testutil.AccPubKeys[0],
		0,
		testutil.ValAddrs[0],
		testutil.ConsPubKeys[0],
		testutil.StakingAmount,
	)
	// Run the staking endblocker to ensure valset is correct in state
	staking.EndBlocker(input.Context, input.StakingKeeper)
	tests := []struct {
		name          string
		requestFunc   func() error
		expectedError error
	}{
		{
			name: "check latest nonce before getting the latest valset",
			requestFunc: func() error {
				_, err := k.GetLatestValset(input.Context)
				return err
			},
			expectedError: types.ErrLatestAttestationNonceStillNotInitialized,
		},
		{
			name: "check latest nonce before getting the current valset",
			requestFunc: func() error {
				_, err := k.GetCurrentValset(input.Context)
				return err
			},
			expectedError: types.ErrLatestAttestationNonceStillNotInitialized,
		},
		{
			name: "check latest nonce before getting latest valset before nonce",
			requestFunc: func() error {
				_, err := k.GetLatestValsetBeforeNonce(input.Context, 1)
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

func TestEVMAddresses(t *testing.T) {
	input := testutil.CreateTestEnvWithoutBlobstreamKeysInit(t)
	k := input.BlobstreamKeeper

	_, exists := k.GetEVMAddress(input.Context, testutil.ValAddrs[0])
	require.False(t, exists)

	// now create the validator
	testutil.CreateValidator(
		t,
		input,
		testutil.AccAddrs[0],
		testutil.AccPubKeys[0],
		0,
		testutil.ValAddrs[0],
		testutil.ConsPubKeys[0],
		testutil.StakingAmount,
	)

	evmAddress, exists := k.GetEVMAddress(input.Context, testutil.ValAddrs[0])
	require.True(t, exists)
	require.Equal(t, types.DefaultEVMAddress(testutil.ValAddrs[0]), evmAddress)

	newEvmAddress := gethcommon.BytesToAddress([]byte("a"))
	k.SetEVMAddress(input.Context, testutil.ValAddrs[0], newEvmAddress)
	checkEvmAddress, exists := k.GetEVMAddress(input.Context, testutil.ValAddrs[0])
	require.True(t, exists)
	require.Equal(t, newEvmAddress, checkEvmAddress)

	// squat the next validators default evm address
	k.SetEVMAddress(input.Context, testutil.ValAddrs[0], types.DefaultEVMAddress(testutil.ValAddrs[1]))

	msgServer := stakingkeeper.NewMsgServerImpl(input.StakingKeeper)
	_, err := msgServer.CreateValidator(input.Context, testutil.NewTestMsgCreateValidator(testutil.ValAddrs[1], testutil.ConsPubKeys[1], testutil.StakingAmount))
	require.Error(t, err)
	require.True(t, errors.Is(err, types.ErrEVMAddressAlreadyExists), err.Error())

	resp, err := k.EVMAddress(input.Context, &types.QueryEVMAddressRequest{
		ValidatorAddress: testutil.ValAddrs[0].String(),
	})
	require.NoError(t, err)
	require.Equal(t, types.DefaultEVMAddress(testutil.ValAddrs[1]).String(), resp.EvmAddress)

	_, err = k.EVMAddress(input.Context, &types.QueryEVMAddressRequest{})
	require.Error(t, err)

	resp, err = k.EVMAddress(input.Context, &types.QueryEVMAddressRequest{
		ValidatorAddress: testutil.ValAddrs[1].String(),
	})
	require.NoError(t, err)
	require.Equal(t, "", resp.EvmAddress)
}
