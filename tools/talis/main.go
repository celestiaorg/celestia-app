package main

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
)

const Version = "0.0.2"

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of talis",
		Long:  "Print the version number of talis",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("talis version %s\n", Version)
		},
	}
}

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
		versionCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
