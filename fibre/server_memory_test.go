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
		connections, streams, perRPC, err := cfg.rpcLimits(rows)
		require.NoError(t, err)
		slots := connections * streams
		require.Greater(t, slots, previous, "smaller assignments should allow more RPCs")
		require.Equal(t, 9, streams)
		require.LessOrEqual(t, int64(slots)*perRPC, cfg.RPCMemoryBudgetMiB<<20)
		if rows == 1721 {
			require.Equal(t, 63, slots, "14%% stake with 32 verifiers")
		}
		previous = slots
	}
	connections, streams, _, err := cfg.rpcLimits(1721)
	require.NoError(t, err)
	cfg.RPCMemoryBudgetMiB *= 2
	moreConnections, moreStreams, _, err := cfg.rpcLimits(1721)
	require.NoError(t, err)
	require.Greater(t, moreConnections*moreStreams, connections*streams)
}

func TestRPCLimitsInsufficientMemory(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.UploadVerifyWorkers = 1
	cfg.RPCMemoryBudgetMiB = 2049
	_, _, _, err := cfg.rpcLimits(4096)
	require.ErrorContains(t, err, "upload_verify_workers")
	cfg.RPCMemoryBudgetMiB = 2200
	_, _, _, err = cfg.rpcLimits(4096)
	require.ErrorContains(t, err, "one RPC")
	cfg.RPCMemoryBudgetMiB = 2700
	connections, streams, _, err := cfg.rpcLimits(4096)
	require.NoError(t, err)
	require.Equal(t, 1, connections)
	require.Equal(t, 1, streams)

	cfg.RPCMemoryBudgetMiB = math.MaxInt64 >> 20
	cfg.UploadVerifyWorkers = math.MaxInt
	_, _, _, err = cfg.rpcLimits(4096)
	require.ErrorContains(t, err, "upload_verify_workers")
}
