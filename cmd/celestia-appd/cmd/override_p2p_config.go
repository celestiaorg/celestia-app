package cmd

import (
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v6/app"
	tmcfg "github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

const (
	mebibyte               = 1048576
	bypassOverridesFlagKey = "bypass-config-overrides"
)

// overrideP2PConfig overrides the P2P send and recv rates to ensure they meet
// the minimum required values, even if the user has configured lower values in
// their config.toml file. If the user has configured higher values, those are
// preserved.
func overrideP2PConfig(cmd *cobra.Command, logger log.Logger) error {
	// Check if overrides should be bypassed
	bypass, err := cmd.Flags().GetBool(bypassOverridesFlagKey)
	if err == nil && bypass {
		logger.Info("Bypassing config overrides due to flag")
		return nil
	}

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

	// Override mempool configs
	overrideMempoolConfig(cfg, defaultCfg, logger)

	return nil
}

// overrideMempoolConfig overrides mempool configuration values to ensure they
// meet the minimum required values or are set to specific values as needed.
func overrideMempoolConfig(cfg, defaultCfg *tmcfg.Config, logger log.Logger) {
	const minTTLNumBlocks = int64(36)
	const minMaxTxsBytes = int64(400 * mebibyte) // 400 MiB

	// Force mempool type to CAT if it's not already set to CAT
	if cfg.Mempool.Type != tmcfg.MempoolTypeCAT {
		logger.Info("Overriding Mempool Type to CAT",
			"configured", cfg.Mempool.Type,
			"default", tmcfg.MempoolTypeCAT,
		)
		cfg.Mempool.Type = tmcfg.MempoolTypeCAT
	}

	// Override TTLNumBlocks if it's less than the minimum and not 0
	// If it's 0, the user has explicitly disabled it, so we leave it alone
	if cfg.Mempool.TTLNumBlocks > 0 && cfg.Mempool.TTLNumBlocks < minTTLNumBlocks {
		logger.Info("Overriding Mempool TTLNumBlocks to minimum",
			"configured", cfg.Mempool.TTLNumBlocks,
			"minimum", minTTLNumBlocks,
		)
		cfg.Mempool.TTLNumBlocks = minTTLNumBlocks
	}

	// Force TTLDuration to 0
	if cfg.Mempool.TTLDuration != 0 {
		logger.Info("Overriding Mempool TTLDuration to 0",
			"configured", cfg.Mempool.TTLDuration,
		)
		cfg.Mempool.TTLDuration = 0
	}

	// Override MaxGossipDelay if it's the old default value (60s)
	const oldMaxGossipDelay = 60 * 1e9 // 60 seconds in nanoseconds
	if cfg.Mempool.MaxGossipDelay == oldMaxGossipDelay {
		logger.Info("Overriding Mempool MaxGossipDelay",
			"configured_seconds", cfg.Mempool.MaxGossipDelay/1e9,
			"new_seconds", defaultCfg.Mempool.MaxGossipDelay/1e9,
		)
		cfg.Mempool.MaxGossipDelay = defaultCfg.Mempool.MaxGossipDelay
	}

	// Override MaxTxBytes if it's less than the default
	if cfg.Mempool.MaxTxBytes < defaultCfg.Mempool.MaxTxBytes {
		logger.Info("Overriding Mempool MaxTxBytes to minimum",
			"configured", cfg.Mempool.MaxTxBytes,
			"minimum", defaultCfg.Mempool.MaxTxBytes,
		)
		cfg.Mempool.MaxTxBytes = defaultCfg.Mempool.MaxTxBytes
	}

	// Override MaxTxsBytes if it's less than the minimum
	if cfg.Mempool.MaxTxsBytes < minMaxTxsBytes {
		logger.Info("Overriding Mempool MaxTxsBytes to minimum",
			"configured_mib", cfg.Mempool.MaxTxsBytes/mebibyte,
			"minimum_mib", minMaxTxsBytes/mebibyte,
		)
		cfg.Mempool.MaxTxsBytes = minMaxTxsBytes
	}
}
