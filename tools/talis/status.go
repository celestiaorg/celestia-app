package main

import "github.com/spf13/cobra"

func statusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check the status of the Talis network",
		Long:  "Check the status of the Talis network using the provided configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	return cmd
}
