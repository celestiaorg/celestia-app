package main

import (
	"log"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "talis",
		Short: "Talis CLI",
		Long:  "Talis CLI is a command line interface for running performance experiments.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	rootCmd.AddCommand(
		downloadCmd(),
		generateCmd(),
		initCmd(),
		statusCmd(),
		listCmd(),
		upCmd(),
		downCmd(),
		deployCmd(),
		addCmd(),
		startTxsimCmd(),
		startLatencyMonitorCmd(),
		uploadDataCmd(),
		killTmuxSessionCmd(),
		resetCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
