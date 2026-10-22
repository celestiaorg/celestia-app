//go:build !multiplexer

package cmd

import (
	"github.com/celestiaorg/celestia-app/v10/app"
	appcodec "github.com/celestiaorg/celestia-app/v10/app/grpc/codec"
	"github.com/celestiaorg/celestia-app/v10/app/observability"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

// modifyRootCommand sets the default root command without adding a multiplexer.
func modifyRootCommand(rootCommand *cobra.Command, capp *app.App) {
	boundedCodec, err := appcodec.New(codec.NewProtoCodec(capp.GetEncodingConfig().InterfaceRegistry).GRPCCodec())
	if err != nil {
		panic(err)
	}

	server.AddCommandsWithStartCmdOptions(
		rootCommand,
		app.NodeHome,
		NewAppServer,
		appExporter,
		server.StartCmdOptions{
			AddFlags: addStartFlags,
			GRPCServerOptions: []grpc.ServerOption{
				grpc.ForceServerCodec(boundedCodec),
				grpc.ChainUnaryInterceptor(observability.UnaryPrometheusInterceptor()),
			},
		},
	)
}
