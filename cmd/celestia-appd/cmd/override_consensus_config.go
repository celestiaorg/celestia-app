package cmd

import (
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// overrideConsensusTimeouts overrides the values set for consensus timeouts.
// Keeping these values overridden as a fallback in case the state didn't return the right values.
func overrideConsensusTimeouts(cmd *cobra.Command, logger log.Logger) error {
	// Check if overrides should be bypassed
	bypass, err := cmd.Flags().GetBool(bypassOverridesFlagKey)
	if err == nil && bypass {
		logger.Info("Bypassing config overrides due to flag")
		return nil
	}

	sctx := server.GetServerContextFromCmd(cmd)
	cfg := sctx.Config

	overrideTimeout(logger, "timeout_propose", &cfg.Consensus.TimeoutPropose, appconsts.TimeoutPropose)
	overrideTimeout(logger, "timeout_prevote", &cfg.Consensus.TimeoutPrevote, appconsts.TimeoutPrevote)
	overrideTimeout(logger, "timeout_prevote_delta", &cfg.Consensus.TimeoutPrevoteDelta, appconsts.TimeoutPrevoteDelta)
	overrideTimeout(logger, "timeout_precommit", &cfg.Consensus.TimeoutPrecommit, appconsts.TimeoutPrecommit)
	overrideTimeout(logger, "timeout_precommit_delta", &cfg.Consensus.TimeoutPrecommitDelta, appconsts.TimeoutPrecommitDelta)
	overrideTimeout(logger, "timeout_commit", &cfg.Consensus.TimeoutCommit, appconsts.TimeoutCommit)

	return nil
}

// overrideTimeout sets a consensus timeout to its enforced value and logs when
// the configured value differs.
func overrideTimeout(logger log.Logger, name string, configured *time.Duration, enforced time.Duration) {
	if *configured != enforced {
		logger.Info("Overriding consensus timeout", "name", name, "configured", configured.String(), "enforced", enforced.String())
		*configured = enforced
	}
}
