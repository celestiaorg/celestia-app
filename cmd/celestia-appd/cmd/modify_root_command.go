//go:build !multiplexer

package cmd

import (
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/observability"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

// modifyRootCommand sets the default root command without adding a multiplexer.
func modifyRootCommand(rootCommand *cobra.Command) {
	server.AddCommandsWithStartCmdOptions(
		rootCommand,
		app.NodeHome,
		NewAppServer,
		appExporter,
		standaloneStartCmdOptions(),
	)
}

// standaloneStartCmdOptions configures the SDK start command for the
// non-multiplexer build.
func standaloneStartCmdOptions() server.StartCmdOptions {
	return server.StartCmdOptions{
		AddFlags: addStartFlags,
		GRPCServerOptions: []grpc.ServerOption{
			grpc.ChainUnaryInterceptor(observability.UnaryPrometheusInterceptor()),
		},
		// QuerySequence reads pending sequences from the node's mempool.
		PreNodeStart: app.WirePendingSequencer,
	}
}
