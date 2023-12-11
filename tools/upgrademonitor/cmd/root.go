package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "upgrademonitor grpc-endpoint",
	Short: "upgrademonitor monitors that status of upgrades on a Celestia network.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("hello")
	},
	Args: cobra.ExactArgs(1),
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
