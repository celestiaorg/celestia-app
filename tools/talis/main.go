package main

import (
	"log"

	"github.com/spf13/cobra"
)

var globalWorkers int

func main() {
	rootCmd := &cobra.Command{
		Use:   "talis",
		Short: "Talis CLI",
		Long:  "Talis CLI is a command line interface for running performance experiments.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	rootCmd.PersistentFlags().IntVarP(&globalWorkers, "workers", "w", 10, "number of concurrent workers for parallel operations")

	rootCmd.AddCommand(
		downloadCmd(),
		generateCmd(),
		initCmd(),
		statusCmd(),
		upCmd(),
		downCmd(),
		deployCmd(),
		addCmd(),
		startTxsimCmd(),
		uploadDataCmd(),
		killTmuxSessionCmd(),
		resetCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
