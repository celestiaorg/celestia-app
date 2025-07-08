package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

type PreStartHook func(cmd *cobra.Command) error

// addPreStartHooks finds the start command and adds pre-start hooks using Cobra's PreRunE
func addPreStartHooks(rootCmd *cobra.Command, hooks ...PreStartHook) error {
	// find start command
	startCmd, _, err := rootCmd.Find([]string{"start"})
	if err != nil {
		return fmt.Errorf("failed to find start command: %w", err)
	}
	
	// Add the pre-start hooks using Cobra's PreRunE
	startCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		// Run all pre-start hooks explicitly
		for _, hook := range hooks {
			if err := hook(cmd); err != nil {
				return err
			}
		}
		return nil
	}
	
	return nil
}
