package cmd

import (
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// overrideConsensusTimeouts overrides the values set for consensus timeouts
// and the peer gossip sleep, so a binary upgrade applies them to nodes whose
// config.toml predates it. Keeping the timeouts overridden is also a fallback
// in case the state didn't return the right values.
func overrideConsensusTimeouts(cmd *cobra.Command, logger log.Logger) error {
	// Check if overrides should be bypassed
	bypass, err := cmd.Flags().GetBool(bypassOverridesFlagKey)
	if err == nil && bypass {
		logger.Info("Bypassing config overrides due to flag")
		return nil
	}

	sctx := server.GetServerContextFromCmd(cmd)
	cfg := sctx.Config

	cfg.Consensus.TimeoutPropose = appconsts.TimeoutPropose
	cfg.Consensus.TimeoutPrevote = appconsts.TimeoutPrevote
	cfg.Consensus.TimeoutPrevoteDelta = appconsts.TimeoutPrevoteDelta
	cfg.Consensus.TimeoutPrecommit = appconsts.TimeoutPrecommit
	cfg.Consensus.TimeoutPrecommitDelta = appconsts.TimeoutPrecommitDelta
	cfg.Consensus.TimeoutCommit = appconsts.TimeoutCommit
	cfg.Consensus.PeerGossipSleepDuration = app.DefaultConsensusConfig().Consensus.PeerGossipSleepDuration

	return nil
}
