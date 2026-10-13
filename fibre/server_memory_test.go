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
		require.GreaterOrEqual(t, limits.connections, limits.streams)
		require.LessOrEqual(t, int64(slots)*limits.bytesPerRPC, cfg.RPCMemoryBudgetMiB<<20)
		if rows == 1721 {
			require.Equal(t, 35, slots, "14%% stake with 32 verifiers")
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

func TestRPCLimitsBalancedSplit(t *testing.T) {
	tests := []struct {
		name        string
		budgetMiB   int64
		rows        int
		connections int
		streams     int
	}{
		{name: "one RPC", budgetMiB: 7000, rows: 4096, connections: 1, streams: 1},
		{name: "two RPCs", budgetMiB: 7300, rows: 4096, connections: 2, streams: 1},
		{name: "maximum shard", budgetMiB: 10240, rows: 4096, connections: 4, streams: 2},
		{name: "14 percent stake", budgetMiB: 10240, rows: 1721, connections: 7, streams: 5},
		{name: "large budget", budgetMiB: 65536, rows: 4096, connections: 14, streams: 10},
		{name: "original ratio", budgetMiB: 90744, rows: 4096, connections: 16, streams: 13},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultServerConfig()
			cfg.UploadVerifyWorkers = 32
			cfg.RPCMemoryBudgetMiB = tt.budgetMiB
			limits, err := calculateRPCLimits(&cfg, tt.rows)
			require.NoError(t, err)
			require.Equal(t, tt.connections, limits.connections)
			require.Equal(t, tt.streams, limits.streams)
		})
	}
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
	limits, err = calculateRPCLimits(&cfg, cfg.MinRowsPerValidator)
	require.NoError(t, err)
	require.Positive(t, limits.connections)
	require.Positive(t, limits.streams)
	require.LessOrEqual(t, int64(limits.connections)*int64(limits.streams), (cfg.RPCMemoryBudgetMiB<<20)/limits.bytesPerRPC)
	cfg.UploadVerifyWorkers = math.MaxInt
	_, err = calculateRPCLimits(&cfg, 4096)
	require.ErrorContains(t, err, "upload_verify_workers")
}
