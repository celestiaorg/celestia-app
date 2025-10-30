package cmd

import (
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

const (
	mebibyte = 1048576
)

// overrideP2PConfig overrides the P2P send and recv rates to ensure they meet
// the minimum required values, even if the user has configured lower values in
// their config.toml file. If the user has configured higher values, those are
// preserved.
func overrideP2PConfig(cmd *cobra.Command, logger log.Logger) error {
	sctx := server.GetServerContextFromCmd(cmd)
	cfg := sctx.Config

	// Get the default config to extract the minimum required values
	defaultCfg := app.DefaultConsensusConfig()
	minSendRate := defaultCfg.P2P.SendRate
	minRecvRate := defaultCfg.P2P.RecvRate

	// Only override if the configured values are lower than the minimum
	if cfg.P2P.SendRate < minSendRate {
		logger.Info("Overriding P2P SendRate to minimum",
			"configured_mib", cfg.P2P.SendRate/mebibyte,
			"minimum_mib", minSendRate/mebibyte,
		)
		cfg.P2P.SendRate = minSendRate
	}

	if cfg.P2P.RecvRate < minRecvRate {
		logger.Info("Overriding P2P RecvRate to minimum",
			"configured_mib", cfg.P2P.RecvRate/mebibyte,
			"minimum_mib", minRecvRate/mebibyte,
		)
		cfg.P2P.RecvRate = minRecvRate
	}

	return nil
}
