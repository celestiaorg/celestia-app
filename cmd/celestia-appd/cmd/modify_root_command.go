//go:build !multiplexer

package cmd

import (
<<<<<<< HEAD
	"github.com/celestiaorg/celestia-app/v8/app"
=======
	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/celestiaorg/celestia-app/v9/app/observability"
>>>>>>> eda077f2 (feat: add gRPC interceptors to app (#7234))
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
			GRPCServerOptions: []grpc.ServerOption{
				grpc.ChainUnaryInterceptor(observability.UnaryPrometheusInterceptor()),
			},
		},
	)
}
