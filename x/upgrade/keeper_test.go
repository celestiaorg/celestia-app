package upgrade_test

import (
	"fmt"
	"math"
	"math/big"
	"testing"

	sdkmath "cosmossdk.io/math"
	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/x/upgrade"
	"github.com/celestiaorg/celestia-app/v2/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetVotingPowerThreshold(t *testing.T) {
	bigInt := big.NewInt(0)
	bigInt.SetString("23058430092136939509", 10)

	type testCase struct {
		name       string
		validators map[string]int64
		want       sdkmath.Int
	}
	testCases := []testCase{
		{
			name:       "empty validators",
			validators: map[string]int64{},
			want:       sdkmath.NewInt(0),
		},
		{
			name:       "one validator with 6 power returns 5 because the defaultSignalThreshold is 5/6",
			validators: map[string]int64{"a": 6},
			want:       sdkmath.NewInt(5),
		},
		{
			name:       "one validator with 11 power (11 * 5/6 = 9.16666667) so should round up to 10",
			validators: map[string]int64{"a": 11},
			want:       sdkmath.NewInt(10),
		},
		{
			name:       "one validator with voting power of math.MaxInt64",
			validators: map[string]int64{"a": math.MaxInt64},
			want:       sdkmath.NewInt(7686143364045646503),
		},
		{
			name:       "multiple validators with voting power of math.MaxInt64",
			validators: map[string]int64{"a": math.MaxInt64, "b": math.MaxInt64, "c": math.MaxInt64},
			want:       sdkmath.NewIntFromBigInt(bigInt),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stakingKeeper := newMockStakingKeeper(tc.validators)
			k := upgrade.NewKeeper(nil, stakingKeeper)
			got := k.GetVotingPowerThreshold(sdk.Context{})
			assert.Equal(t, tc.want, got, fmt.Sprintf("want %v, got %v", tc.want.String(), got.String()))
		})
	}
}

// TestResetTally verifies that the VotingPower for all versions is reset to
// zero after calling ResetTally.
func TestResetTally(t *testing.T) {
	upgradeKeeper, ctx, _ := setup(t)

	upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[0].String(), Version: 2})
	resp, err := upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{Version: 2})
	require.NoError(t, err)
	assert.Equal(t, uint64(40), resp.VotingPower)

	upgradeKeeper.SignalVersion(ctx, &types.MsgSignalVersion{ValidatorAddress: testutil.ValAddrs[1].String(), Version: 3})
	resp, err = upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{Version: 3})
	require.NoError(t, err)
	assert.Equal(t, uint64(0), resp.VotingPower)

	upgradeKeeper.ResetTally(ctx)

	resp, err = upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{Version: 2})
	require.NoError(t, err)
	assert.Equal(t, uint64(0), resp.VotingPower)

	resp, err = upgradeKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{Version: 3})
	require.NoError(t, err)
	assert.Equal(t, uint64(0), resp.VotingPower)
}
