package fibre

import (
	"fmt"
	"math"
)

type rpcLimits struct {
	connections int
	streams     int
	bytesPerRPC int64
}

// calculateRPCLimits converts the configured MiB budget (1024 MiB = 1 GiB) into
// connection and stream limits. It first reserves 2 GiB for runtime and storage,
// plus one estimated message and 4 MiB of workspace per upload verifier.
//
// The message estimate scales MaxMessageSize by assignedRows / OriginalRows,
// with 1 MiB added for fixed metadata. Each RPC costs three message copies plus
// 5 MiB of overhead. The remaining budget divided by that cost gives the RPC count.
// For N RPCs, connections = ceil(sqrt(N * 16 / 13)), capped at N, and
// streams = floor(N / connections). This approximates the original 16:13 ratio
// between connections and streams per connection without exceeding the RPC count.
// This rough upload estimate also covers downloads of the expected shard size.
func calculateRPCLimits(cfg *ServerConfig, assignedRows int) (rpcLimits, error) {
	const (
		mib               = int64(1 << 20)
		runtimeHeadroom   = 2 * 1024 * mib
		messageOverhead   = mib
		verifierWorkspace = 4 * mib
		rpcOverhead       = 5 * mib
	)

	bytesPerRow := int64(cfg.MaxMessageSize)/int64(cfg.OriginalRows) + 1
	messageBytes := bytesPerRow*int64(assignedRows) + messageOverhead
	bytesPerRPC := 3*messageBytes + rpcOverhead
	bytesPerVerifier := messageBytes + verifierWorkspace

	budgetBytes := cfg.RPCMemoryBudgetMiB * mib
	availableBytes := budgetBytes - runtimeHeadroom
	verifiers := int64(cfg.UploadVerifyWorkers)

	// Divide before multiplying so a large worker count cannot overflow.
	if verifiers > availableBytes/bytesPerVerifier {
		return rpcLimits{}, fmt.Errorf("rpc_memory_budget_mib is too small for %d upload_verify_workers", cfg.UploadVerifyWorkers)
	}
	availableBytes -= verifiers * bytesPerVerifier

	rpcCount := availableBytes / bytesPerRPC
	if rpcCount < 1 {
		return rpcLimits{}, fmt.Errorf("rpc_memory_budget_mib is too small for one RPC after verifier and runtime headroom")
	}

	const connectionStreamRatio = 16.0 / 13.0
	connections := min(rpcCount, int64(math.Ceil(math.Sqrt(float64(rpcCount)*connectionStreamRatio))))
	streams := rpcCount / connections
	return rpcLimits{
		connections: int(connections),
		streams:     int(streams),
		bytesPerRPC: bytesPerRPC,
	}, nil
}
