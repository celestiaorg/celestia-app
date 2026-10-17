package cmd

import (
	"time"

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

	overrideDuration(logger, "timeout_propose", &cfg.Consensus.TimeoutPropose, appconsts.TimeoutPropose)
	overrideDuration(logger, "timeout_prevote", &cfg.Consensus.TimeoutPrevote, appconsts.TimeoutPrevote)
	overrideDuration(logger, "timeout_prevote_delta", &cfg.Consensus.TimeoutPrevoteDelta, appconsts.TimeoutPrevoteDelta)
	overrideDuration(logger, "timeout_precommit", &cfg.Consensus.TimeoutPrecommit, appconsts.TimeoutPrecommit)
	overrideDuration(logger, "timeout_precommit_delta", &cfg.Consensus.TimeoutPrecommitDelta, appconsts.TimeoutPrecommitDelta)
	overrideDuration(logger, "timeout_commit", &cfg.Consensus.TimeoutCommit, appconsts.TimeoutCommit)
	overrideDuration(logger, "peer_gossip_sleep_duration", &cfg.Consensus.PeerGossipSleepDuration,
		app.DefaultConsensusConfig().Consensus.PeerGossipSleepDuration)

	return nil
}

// overrideDuration sets a consensus duration to its enforced value and logs
// when the configured value differs.
func overrideDuration(logger log.Logger, name string, configured *time.Duration, enforced time.Duration) {
	if *configured != enforced {
		logger.Info("Overriding consensus config value", "name", name, "configured", configured.String(), "enforced", enforced.String())
		*configured = enforced
	}
}
