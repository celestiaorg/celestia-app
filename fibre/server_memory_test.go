package fibre

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRPCLimits(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.UploadVerifyWorkers = 32
	var previous int
	for _, rows := range []int{4096, 1721, 615, cfg.MinRowsPerValidator} {
		limits, err := calculateRPCLimits(&cfg, rows)
		require.NoError(t, err)
		slots := limits.connections * limits.streams
		require.Greater(t, slots, previous, "smaller assignments should allow more RPCs")
		require.Equal(t, 9, limits.streams)
		require.LessOrEqual(t, int64(slots)*limits.bytesPerRPC, cfg.RPCMemoryBudgetMiB<<20)
		if rows == 1721 {
			require.Equal(t, 27, slots, "14%% stake with 32 verifiers")
		}
		previous = slots
	}
	limits, err := calculateRPCLimits(&cfg, 1721)
	require.NoError(t, err)
	cfg.RPCMemoryBudgetMiB *= 2
	more, err := calculateRPCLimits(&cfg, 1721)
	require.NoError(t, err)
	require.Greater(t, more.connections*more.streams, limits.connections*limits.streams)
}

func TestRPCLimitsInsufficientMemory(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.UploadVerifyWorkers = 1
	cfg.RPCMemoryBudgetMiB = 2049
	_, err := calculateRPCLimits(&cfg, 4096)
	require.ErrorContains(t, err, "upload_verify_workers")
	cfg.RPCMemoryBudgetMiB = 2200
	_, err = calculateRPCLimits(&cfg, 4096)
	require.ErrorContains(t, err, "one RPC")
	cfg.RPCMemoryBudgetMiB = 2700
	limits, err := calculateRPCLimits(&cfg, 4096)
	require.NoError(t, err)
	require.Equal(t, 1, limits.connections)
	require.Equal(t, 1, limits.streams)

	cfg.RPCMemoryBudgetMiB = math.MaxInt64 >> 20
	cfg.UploadVerifyWorkers = math.MaxInt
	_, err = calculateRPCLimits(&cfg, 4096)
	require.ErrorContains(t, err, "upload_verify_workers")
}
