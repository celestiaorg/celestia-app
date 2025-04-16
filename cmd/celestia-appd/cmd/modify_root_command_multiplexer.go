//go:build multiplexer

package cmd

import (
	"github.com/spf13/cobra"

	"github.com/celestiaorg/celestia-app/multiplexer/abci"
	"github.com/celestiaorg/celestia-app/multiplexer/appd"
	multiplexer "github.com/celestiaorg/celestia-app/multiplexer/cmd"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/cosmos/cosmos-sdk/server"

	embedding "github.com/celestiaorg/celestia-app/v4/internal/embedding"
)

// modifyRootCommand enhances the root command with the pass through and multiplexer.
func modifyRootCommand(rootCommand *cobra.Command) {
	v3AppBinary, err := embedding.CelestiaAppV3()
	if err != nil {
		panic(err)
	}

	v3, err := appd.New("v3", v3AppBinary)
	if err != nil {
		panic(err)
	}

	versions, err := abci.NewVersions(abci.Version{
		Appd:        v3,
		ABCIVersion: abci.ABCIClientVersion1,
		AppVersion:  3,
		StartArgs: []string{
			"--grpc.enable",
			"--grpc.address=0.0.0.0:9090", // ensure the grpc address is accessible from hosts such as txsim. (not just localhost)
			"--api.enable",
			"--api.swagger=false",
			"--with-tendermint=false",
			"--transport=grpc",
			"--address=0.0.0.0:26658",
			// "--v2-upgrade-height=0",
		},
	})
	if err != nil {
		panic(err)
	}

	rootCommand.AddCommand(
		multiplexer.NewPassthroughCmd(versions),
	)

	// Add the following commands to the rootCommand: start, tendermint, export, version, and rollback and wire multiplexer.
	server.AddCommandsWithStartCmdOptions(
		rootCommand,
		app.DefaultNodeHome,
		NewAppServer,
		appExporter,
		server.StartCmdOptions{
			AddFlags:            addStartFlags,
			StartCommandHandler: multiplexer.New(versions),
		},
	)
}
