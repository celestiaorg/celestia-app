//go:build !multiplexer

package cmd

import (
	"context"

	"github.com/celestiaorg/celestia-app/v12/app"
	"github.com/celestiaorg/celestia-app/v12/app/appdb"
	"github.com/celestiaorg/celestia-app/v12/app/observability"
	"github.com/celestiaorg/celestia-app/v12/internal/version"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/proxy"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
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
	var consensusNode *node.Node
	return server.StartCmdOptions{
		AddFlags: addStartFlags,
		DBOpener: appdb.Open,
		GRPCServerOptions: []grpc.ServerOption{
			grpc.ChainUnaryInterceptor(observability.UnaryPrometheusInterceptor()),
		},
		// QuerySequence reads pending sequences from the node's mempool.
		PreNodeStart: func(a servertypes.Application, n *node.Node) error {
			consensusNode = n
			return app.WirePendingSequencer(a, n)
		},
		PostSetup: func(sctx *server.Context, _ client.Context, ctx context.Context, g *errgroup.Group) error {
			g.Go(func() error {
				version.Run(ctx, consensusNode, sctx.Viper, sctx.Logger)
				return nil
			})
			return nil
		},
		// One mutex per ABCI connection, as the multiplexer build wires it, so
		// CheckTx does not wait for PrepareProposal, ProcessProposal or FinalizeBlock.
		ClientCreator: proxy.NewConnSyncLocalClientCreator,
	}
}
