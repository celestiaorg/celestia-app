package fibre

import (
	"context"
	"fmt"
)

// configureRPCLimits sizes the transport once, before it starts accepting requests.
func (s *Server) configureRPCLimits(ctx context.Context) error {
	set, err := s.state.Head(ctx)
	if err != nil {
		return fmt.Errorf("fetching validator set: %w", err)
	}
	key, err := s.signer.GetPubKey()
	if err != nil {
		return fmt.Errorf("getting validator public key: %w", err)
	}
	rows := s.Config.OriginalRows
	if val, found := set.GetByAddress(key.Address()); found {
		rows = set.AssignedRows(val, s.Config.OriginalRows, s.Config.MinRowsPerValidator, s.Config.LivenessThreshold)
	} else {
		s.log.Warn("validator absent from startup set; sizing RPC concurrency for the maximum shard")
	}

	connections, streams, perRPC, err := s.Config.rpcLimits(rows)
	if err != nil {
		return err
	}
	s.Config.MaxConnections = connections
	s.Config.MaxConcurrentStreams = streams
	s.log.Info("RPC concurrency sized from startup assignment",
		"memory_budget_mib", s.Config.RPCMemoryBudgetMiB,
		"assigned_rows", rows, "validator_set_height", set.Height,
		"estimated_bytes_per_rpc", perRPC,
		"max_connections", connections, "max_concurrent_streams", streams,
	)
	return nil
}

// rpcLimits estimates upload memory; the same conservative limit covers downloads.
func (cfg ServerConfig) rpcLimits(rows int) (connections, streams int, perRPC int64, err error) {
	// Scale the protocol message limit by assignment. The extra MiB covers fixed
	// metadata (including RLCs) which does not shrink with the row count.
	messageBytes := (int64(cfg.MaxMessageSize)/int64(cfg.OriginalRows)+1)*int64(rows) + 1<<20
	perRPC = 3*messageBytes + 5<<20                     // receive buffers, materialised copy, decoded shard, overhead
	perVerifier := messageBytes + 4<<20                 // retained shard references and verifier workspace
	remaining := (cfg.RPCMemoryBudgetMiB << 20) - 2<<30 // runtime and storage headroom
	// Divide before multiplying so a large worker count cannot overflow.
	if int64(cfg.UploadVerifyWorkers) > remaining/perVerifier {
		return 0, 0, 0, fmt.Errorf("rpc_memory_budget_mib is too small for %d upload_verify_workers", cfg.UploadVerifyWorkers)
	}
	remaining -= int64(cfg.UploadVerifyWorkers) * perVerifier
	slots := remaining / perRPC
	if slots < 1 {
		return 0, 0, 0, fmt.Errorf("rpc_memory_budget_mib is too small for one RPC after verifier and runtime headroom")
	}
	streams = int(min(slots, 9))
	connections = int(slots / int64(streams))
	return connections, streams, perRPC, nil
}
