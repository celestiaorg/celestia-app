package cmd

import (
	"fmt"
	"sort"
	"strings"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v8/app"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/reflect/protoreflect"
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

			// Get all registered interfaces and their implementations
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

			// Get all protobuf types from file descriptors
			var allProtoTypes []string
			var eventTypes []string
			var messageTypes []string

			registry.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
				messages := fd.Messages()
				for i := 0; i < messages.Len(); i++ {
					msg := messages.Get(i)
					fullName := string(msg.FullName())
					allProtoTypes = append(allProtoTypes, fullName)

					// Categorize event types vs other message types
					if strings.Contains(strings.ToLower(fullName), "event") {
						eventTypes = append(eventTypes, fullName)
					} else {
						messageTypes = append(messageTypes, fullName)
					}
				}
				return true
			})

			sort.Strings(allProtoTypes)
			sort.Strings(eventTypes)
			sort.Strings(messageTypes)

			// Print event types
			fmt.Println("=== Event Types ===")
			for _, eventType := range eventTypes {
				fmt.Printf("Event: %s\n", eventType)
			}
			fmt.Println()

			// Print all protobuf message types
			fmt.Println("=== All Protobuf Message Types ===")
			for _, msgType := range allProtoTypes {
				fmt.Printf("Message: %s\n", msgType)
			}
			fmt.Println()

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
			fmt.Printf("Total Protobuf Message Types: %d\n", len(allProtoTypes))
			fmt.Printf("Total Event Types: %d\n", len(eventTypes))

			return nil
		},
	}

	return cmd
}
