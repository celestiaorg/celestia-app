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
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}

	rootCmd.AddCommand(
		// downloadCmd(),
		generateCmd(),
		initCmd(),
		// statusCmd(),
		upCmd(),
		downCmd(),
		deployCmd(),
		addCmd(),
		startTxsimCmd(),
		collectTracesCmd(),
		killTmuxSessionCmd(),
		downloadDataCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
