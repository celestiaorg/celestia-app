package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/spf13/cobra"
)

const flagHome = "home"

func newRootCmd() *cobra.Command {
	var (
		traceShutdown   func(context.Context)
		metricsShutdown func(context.Context)
		pprofStop       func()
		pyroStop        func()
	)

	rootCmd := &cobra.Command{
		Use:          "fibre",
		Short:        "Run the Fibre data availability server",
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := setupLogging(cmd); err != nil {
				return err
			}
			shutdown, err := setupTracing(cmd.Context(), cmd)
			if err != nil {
				return err
			}
			traceShutdown = shutdown

			mShutdown, err := setupMetrics(cmd.Context(), cmd)
			if err != nil {
				return err
			}
			metricsShutdown = mShutdown

			stop, err := setupPProfServer(cmd)
			if err != nil {
				return err
			}
			pprofStop = stop

			stop, err = setupProfiling(cmd)
			if err != nil {
				return err
			}
			pyroStop = stop

			return nil
		},
		PersistentPostRunE: func(_ *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			traceShutdown(ctx)
			metricsShutdown(ctx)
			pprofStop()
			pyroStop()
			return nil
		},
	}
	rootCmd.PersistentFlags().String(flagHome, fibre.DefaultHome(), fmt.Sprintf("fibre home directory (or set %s)", fibre.EnvHome))
	if home, ok := os.LookupEnv(fibre.EnvHome); ok && home != "" {
		if err := rootCmd.PersistentFlags().Lookup(flagHome).Value.Set(home); err != nil {
			fmt.Printf("Error setting home directory from %s: %v\n", fibre.EnvHome, err)
			os.Exit(1)
		}
	}

	registerLogFlags(rootCmd)
	registerTracingFlags(rootCmd)
	registerProfilingFlags(rootCmd)

	rootCmd.AddCommand(
		newStartCmd(startServer),
		newVersionCmd(),
	)

	return rootCmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print fibre version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("version: %s\n", version)
			fmt.Printf("commit: %s\n", commit)
			fmt.Printf("build date: %s\n", buildDate)
			fmt.Printf("system: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			fmt.Printf("go: %s\n", runtime.Version())
		},
	}
}
