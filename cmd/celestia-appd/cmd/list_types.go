package cmd

import (
	"fmt"
	"sort"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/spf13/cobra"
	dbm "github.com/cosmos/cosmos-db"
	"cosmossdk.io/log"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
)

// listTypesCmd returns a command that lists all registered SDK messages, events, and proto types
func listTypesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-types",
		Short: "List all registered SDK messages, events, and proto types",
		Long: `Lists all registered protobuf types from the encoding configuration.
This includes SDK messages, events, and other proto types that are registered
in the interface registry.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create a temporary app instance to access the encoding config
			opts := simtestutil.NewAppOptionsWithFlagHome(app.NodeHome)
			tempApp := app.New(log.NewNopLogger(), dbm.NewMemDB(), nil, 0, opts)
			encConfig := tempApp.GetEncodingConfig()
			registry := encConfig.InterfaceRegistry

			// Get all registered interfaces
			interfaces := registry.ListAllInterfaces()
			sort.Strings(interfaces)

			fmt.Println("=== All Registered Interfaces ===")
			for _, iface := range interfaces {
				fmt.Printf("Interface: %s\n", iface)
				
				// Get implementations for this interface
				implementations := registry.ListImplementations(iface)
				sort.Strings(implementations)
				
				for _, impl := range implementations {
					fmt.Printf("  Implementation: %s\n", impl)
				}
				fmt.Println()
			}

			// Print summary statistics
			totalInterfaces := len(interfaces)
			totalImplementations := 0
			for _, iface := range interfaces {
				implementations := registry.ListImplementations(iface)
				totalImplementations += len(implementations)
			}

			fmt.Println("=== Summary ===")
			fmt.Printf("Total Interfaces: %d\n", totalInterfaces)
			fmt.Printf("Total Implementations: %d\n", totalImplementations)

			return nil
		},
	}

	return cmd
}