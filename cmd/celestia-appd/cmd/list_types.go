package cmd

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// typedEventNameRegexp matches the simple (unqualified) name of a typed
// event: "Event" followed by an upper-case letter, e.g. EventPayForBlobs.
// It rejects "Event" itself (tendermint.abci.Event) and names that merely
// contain "Event" such as StringEvent, ValidatorSlashEvent or
// GetTxsEventRequest.
var typedEventNameRegexp = regexp.MustCompile(`^Event[A-Z]`)

// listTypesCmd returns a command that lists all registered SDK messages, events, and proto types
func listTypesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-types",
		Short: "List all registered SDK messages, events, and proto types",
		Long: `Lists all registered protobuf types from the encoding configuration.
This includes SDK messages, events, and other proto types that are registered
in the interface registry.

The "Event Types" section lists typed (protobuf) events only. A proto message
is treated as a typed event when its simple name starts with "Event" followed
by an upper-case letter (e.g. EventPayForBlobs, but not Event, StringEvent or
GetTxsEventRequest) and its proto package, or a parent package, registers at
least one cosmos.base.v1beta1.Msg implementation. The second rule drops events
of modules whose proto files are compiled in but which are not wired into the
app (e.g. cosmos.group.v1). Untyped string events are not listed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create a temporary app instance to access the encoding config
			opts := simtestutil.NewAppOptionsWithFlagHome(app.NodeHome)
			tempApp := app.New(log.NewNopLogger(), dbm.NewMemDB(), nil, 0, 0, opts)
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

			// Proto packages that register at least one sdk.Msg. A module that
			// is wired into the app registers its Msgs; a module whose proto
			// files are merely compiled in does not.
			msgPackages := msgProtoPackages(registry.ListImplementations(sdk.MsgInterfaceProtoName))

			// Get all protobuf types from file descriptors
			var allProtoTypes []string
			var eventTypes []string

			registry.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
				messages := fd.Messages()
				for i := 0; i < messages.Len(); i++ {
					msg := messages.Get(i)
					fullName := msg.FullName()
					allProtoTypes = append(allProtoTypes, string(fullName))

					if isTypedEvent(fullName, msgPackages) {
						eventTypes = append(eventTypes, string(fullName))
					}
				}
				return true
			})

			sort.Strings(allProtoTypes)
			sort.Strings(eventTypes)

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

// msgProtoPackages returns the set of proto packages of the given sdk.Msg type
// URLs (e.g. "/celestia.blob.v1.MsgPayForBlobs" -> "celestia.blob.v1").
func msgProtoPackages(msgTypeURLs []string) map[protoreflect.FullName]bool {
	packages := make(map[protoreflect.FullName]bool, len(msgTypeURLs))
	for _, typeURL := range msgTypeURLs {
		fullName := protoreflect.FullName(strings.TrimPrefix(typeURL, "/"))
		if !fullName.IsValid() {
			continue
		}
		packages[fullName.Parent()] = true
	}
	return packages
}

// isTypedEvent reports whether the proto message fullName is a typed event
// emitted by a module wired into the app: its simple name matches
// typedEventNameRegexp and its package, or a parent package, is in
// msgPackages (the packages that register at least one sdk.Msg).
func isTypedEvent(fullName protoreflect.FullName, msgPackages map[protoreflect.FullName]bool) bool {
	if !typedEventNameRegexp.MatchString(string(fullName.Name())) {
		return false
	}
	for pkg := fullName.Parent(); pkg != ""; pkg = pkg.Parent() {
		if msgPackages[pkg] {
			return true
		}
	}
	return false
}
