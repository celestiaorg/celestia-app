package fibre

import "fmt"

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
// Connections are rounded down with up to nine streams each, so their product fits.
// This rough upload estimate also covers downloads of the expected shard size.
func calculateRPCLimits(cfg *ServerConfig, assignedRows int) (rpcLimits, error) {
	const (
		mib                  = int64(1 << 20)
		runtimeHeadroom      = 2 * 1024 * mib
		messageOverhead      = mib
		verifierWorkspace    = 4 * mib
		rpcOverhead          = 5 * mib
		streamsPerConnection = 9
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

	streams := min(rpcCount, streamsPerConnection)
	return rpcLimits{
		connections: int(rpcCount / streams),
		streams:     int(streams),
		bytesPerRPC: bytesPerRPC,
	}, nil
}
