//go:build !multiplexer

package cmd

import (
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/appdb"
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
		server.StartCmdOptions{
			AddFlags: addStartFlags,
			DBOpener: appdb.Open,
			GRPCServerOptions: []grpc.ServerOption{
				grpc.ChainUnaryInterceptor(observability.UnaryPrometheusInterceptor()),
			},
		},
	)
}
