package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	e2e "github.com/celestiaorg/celestia-app/test/e2e/pkg"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

func main() {
	NewCLI().Run()
}

type CLI struct {
	root    *cobra.Command
	testnet *e2e.Testnet
}

func NewCLI() *CLI {
	cli := &CLI{}
	cli.root = &cobra.Command{
		Use:          "e2e",
		Short:        "Command line runner for celestia app e2e framework",
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			file, err := cmd.Flags().GetString("file")
			if err != nil {
				return err
			}

			manifest, err := e2e.LoadManifest(file)
			if err != nil {
				return fmt.Errorf("loading manifest: %w", err)
			}

			testnet, err := e2e.LoadTestnet(manifest, file)
			if err != nil {
				return fmt.Errorf("building testnet: %w", err)
			}

			cli.testnet = testnet
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := e2e.Cleanup(ctx, cli.testnet); err != nil {
				return fmt.Errorf("preparing testnet: %w", err)
			}

			if err := e2e.Setup(ctx, cli.testnet); err != nil {
				return fmt.Errorf("setting up testnet: %w", err)
			}
			defer func() { _ = e2e.Cleanup(ctx, cli.testnet) }()

			if err := e2e.Start(ctx, cli.testnet); err != nil {
				return fmt.Errorf("starting network: %w", err)
			}

			if err := e2e.WaitForNBlocks(ctx, cli.testnet, 10); err != nil {
				return fmt.Errorf("waiting for the network to produce blocks: %w", err)
			}

			if err := e2e.Stop(ctx, cli.testnet); err != nil {
				return fmt.Errorf("stopping network: %w", err)
			}

			fmt.Println("Finished testnet successfully")
			return nil
		},
	}
	cli.root.PersistentFlags().StringP("file", "f", "", "Testnet TOML manifest")
	_ = cli.root.MarkPersistentFlagRequired("file")

	cli.root.AddCommand(&cobra.Command{
		Use:   "setup",
		Short: "Setups a testnet",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return e2e.Setup(cmd.Context(), cli.testnet)
		},
	})

	cli.root.AddCommand(&cobra.Command{
		Use:   "start",
		Short: "Starts a testnet",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return e2e.Start(cmd.Context(), cli.testnet)
		},
	})

	cli.root.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stops a testnet",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return e2e.Stop(cmd.Context(), cli.testnet)
		},
	})

	cli.root.AddCommand(&cobra.Command{
		Use:   "cleanup",
		Short: "Tears down network and removes all resources",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return e2e.Cleanup(cmd.Context(), cli.testnet)
		},
	})

	return cli
}

func (cli *CLI) Run() {
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()
	if err := cli.root.ExecuteContext(ctx); err != nil {
		log.Err(err)
		os.Exit(1)
	}
}
