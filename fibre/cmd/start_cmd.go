package main

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	flagAppGRPCAddress      = "app-grpc-address"
	flagServerListenAddress = "server-listen-address"
	flagSignerGRPCAddress   = "signer-grpc-address"

	flagUploadRateLimitEnabled = "upload-rate-limit-enabled"
	flagUploadRateLimitBPS     = "upload-rate-limit-bytes-per-second"
	flagUploadRateLimitBurst   = "upload-rate-limit-burst-bytes"
	flagUploadRateLimitMaxWait = "upload-rate-limit-max-wait"
	flagMaxUploadShardInFlight = "max-upload-shard-in-flight"
)

// newStartCmd builds the "start" subcommand. The start function is called in
// RunE after config resolution; passing it as a parameter keeps the command
// testable without global state.
func newStartCmd(start func(context.Context, fibre.ServerConfig) error) *cobra.Command {
	cfg := fibre.DefaultServerConfig()

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the standalone fibre server, initializing home dir and config (first run only)",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			home, err := cmd.Flags().GetString(flagHome)
			if err != nil {
				return fmt.Errorf("get %q flag: %w", flagHome, err)
			}

			if err := initServerConfig(home); err != nil {
				return err
			}

			// save user-set flag values before Load overwrites them.
			overrides := map[string]string{}
			cmd.Flags().Visit(func(f *pflag.Flag) {
				// NOTE: This doesn't support slice values, support for which can be added if we ever add slice flags.
				overrides[f.Name] = f.Value.String()
			})
			if err := cfg.Load(fibre.DefaultConfigPath(home)); err != nil {
				return err
			}
			// restore user-set flags over loaded config values.
			for name, val := range overrides {
				if err := cmd.Flags().Set(name, val); err != nil {
					return err
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := cmd.Flags().GetString(flagHome)
			if err != nil {
				return fmt.Errorf("get %q flag: %w", flagHome, err)
			}

			cfg.Path = home
			return start(cmd.Context(), cfg)
		},
	}

	// flags point directly to config fields. Defaults shown in --help come from
	// DefaultServerConfig. PreRunE loads the config file (respecting --home),
	// then restores any user-set flags so precedence is: flag > config file > default.
	cmd.Flags().StringVar(&cfg.AppGRPCAddress, flagAppGRPCAddress, cfg.AppGRPCAddress, "core/app node gRPC address")
	cmd.Flags().StringVar(&cfg.ServerListenAddress, flagServerListenAddress, cfg.ServerListenAddress, "fibre server listen address")
	cmd.Flags().StringVar(&cfg.SignerGRPCAddress, flagSignerGRPCAddress, cfg.SignerGRPCAddress, "validator PrivValidatorAPI gRPC address for signing")

	// Upload admission controller. Active only when enabled AND rate > 0.
	cmd.Flags().BoolVar(&cfg.UploadRateLimitEnabled, flagUploadRateLimitEnabled, cfg.UploadRateLimitEnabled, "enable the upload admission controller (rate limit and in-flight cap)")
	cmd.Flags().IntVar(&cfg.UploadRateLimitBytesPerSecond, flagUploadRateLimitBPS, cfg.UploadRateLimitBytesPerSecond, "upload admission rate in bytes/sec (charged as whole-blob UploadSize); <= 0 disables the controller")
	cmd.Flags().IntVar(&cfg.UploadRateLimitBurstBytes, flagUploadRateLimitBurst, cfg.UploadRateLimitBurstBytes, "token-bucket burst in bytes (defaults to the maximum blob size)")
	cmd.Flags().StringVar(&cfg.UploadRateLimitMaxWait, flagUploadRateLimitMaxWait, cfg.UploadRateLimitMaxWait, "max time a request waits for byte budget before ResourceExhausted, as a Go duration (e.g. \"12.8s\")")
	cmd.Flags().IntVar(&cfg.MaxUploadShardInFlight, flagMaxUploadShardInFlight, cfg.MaxUploadShardInFlight, "max concurrent UploadShard handlers admitted past verification")

	return cmd
}
