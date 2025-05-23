package main

import (
	"github.com/spf13/cobra"
)

func downloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download a file from the Talis network",
		Long:  "Download a file from the Talis network using the provided URL.",
	}

	cmd.AddCommand(
		downloadTracesCmd(),
		downloadLogsCmd(),
	)

	return cmd
}

func downloadTracesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "traces",
		Short: "Download configured traces from the Talis network",
		Long:  "Download traces from the Talis network using the provided URL.",
		RunE: func(cmd *cobra.Command, args []string) error {

			return nil
		},
	}

	return cmd
}

func downloadLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Download configured logs from the Talis network",
		Long:  "Download logs from the Talis network using the provided URL.",
		RunE: func(cmd *cobra.Command, args []string) error {

			return nil
		},
	}

	return cmd
}
