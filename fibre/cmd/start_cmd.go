package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/celestiaorg/celestia-app/v8/fibre"
	"github.com/cometbft/cometbft/privval"
	core "github.com/cometbft/cometbft/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	flagAppGRPCAddress      = "app-grpc-address"
	flagServerListenAddress = "server-listen-address"
	flagSignerListenAddress = "signer-listen-address"
	flagFileSigner = "file-signer"
	flagAppHome             = "app-home"
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

			useFileSigner, err := cmd.Flags().GetBool(flagFileSigner)
			if err != nil {
				return fmt.Errorf("get %q flag: %w", flagFileSigner, err)
			}

			// Use file-based signer from the celestia-app home directory
			if useFileSigner {
				appHome, err := cmd.Flags().GetString(flagAppHome)
				if err != nil || appHome == "" {
					appHome = filepath.Join("/root", ".celestia-app")
				}
				pvKeyFile := filepath.Join(appHome, "config", "priv_validator_key.json")
				pvStateFile := filepath.Join(appHome, "data", "priv_validator_state.json")
				if _, err := os.Stat(pvKeyFile); err != nil {
					return fmt.Errorf("validator key file not found: %w", err)
				}
				if _, err := os.Stat(pvStateFile); err != nil {
					return fmt.Errorf("validator state file not found: %w", err)
				}
				slog.Info("using file-based signer", "key", pvKeyFile, "state", pvStateFile)
				filePV := privval.LoadFilePV(pvKeyFile, pvStateFile)
				cfg.SignerFn = func(_ string) (core.PrivValidator, error) {
					return filePV, nil
				}
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
	cmd.Flags().StringVar(&cfg.SignerListenAddress, flagSignerListenAddress, cfg.SignerListenAddress, "privval signer listen address")
	cmd.Flags().Bool(flagFileSigner, false, "use file-based signer from celestia-app home instead of remote signer")
	cmd.Flags().String(flagAppHome, "", "celestia-app home directory for file-based signer (default: /root/.celestia-app)")

	return cmd
}
