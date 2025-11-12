package cmd

import (
	"time"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// overrideConsensusTimeouts overrides the values set for consensus timeouts.
// temporary fix until https://github.com/celestiaorg/celestia-app/issues/6178 is fixed.
func overrideConsensusTimeouts(cmd *cobra.Command, logger log.Logger) error {
	// Check if overrides should be bypassed
	bypass, err := cmd.Flags().GetBool(bypassOverridesFlagKey)
	if err == nil && bypass {
		logger.Info("Bypassing config overrides due to flag")
		return nil
	}

	sctx := server.GetServerContextFromCmd(cmd)
	cfg := sctx.Config

	cfg.Consensus.TimeoutPropose = 3500 * time.Millisecond
	cfg.Consensus.TimeoutPrevote = time.Second
	cfg.Consensus.TimeoutPrevoteDelta = 500 * time.Millisecond
	cfg.Consensus.TimeoutPrecommit = time.Second
	cfg.Consensus.TimeoutPrecommitDelta = 500 * time.Millisecond
	cfg.Consensus.TimeoutCommit = 4200 * time.Millisecond

	return nil
}
