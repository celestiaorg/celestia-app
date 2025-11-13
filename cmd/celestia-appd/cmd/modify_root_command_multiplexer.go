//go:build multiplexer

package cmd

import (
	"github.com/celestiaorg/celestia-app/v6/app"
	embedding "github.com/celestiaorg/celestia-app/v6/internal/embedding"
	"github.com/celestiaorg/celestia-app/v6/multiplexer/abci"
	"github.com/celestiaorg/celestia-app/v6/multiplexer/appd"
	multiplexer "github.com/celestiaorg/celestia-app/v6/multiplexer/cmd"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

var defaultArgs = []string{
	"--with-tendermint=false",
	"--transport=grpc",
}

// modifyRootCommand enhances the root command with the pass through and multiplexer.
func modifyRootCommand(rootCommand *cobra.Command) {
	v3Tag, v3CompressedBinary, err := embedding.CelestiaAppV3()
	if err != nil {
		panic(err)
	}

	appdV3, err := appd.New(v3Tag, v3CompressedBinary)
	if err != nil {
		panic(err)
	}

	v4Tag, v4CompressedBinary, err := embedding.CelestiaAppV4()
	if err != nil {
		panic(err)
	}

	appdV4, err := appd.New(v4Tag, v4CompressedBinary)
	if err != nil {
		panic(err)
	}

	v5Tag, v5CompressedBinary, err := embedding.CelestiaAppV5()
	if err != nil {
		panic(err)
	}

	appdV5, err := appd.New(v5Tag, v5CompressedBinary)
	if err != nil {
		panic(err)
	}

	versions, err := abci.NewVersions(
		abci.Version{
			Appd:        appdV3,
			ABCIVersion: abci.ABCIClientVersion1,
			AppVersion:  3,
			StartArgs:   defaultArgs,
		}, abci.Version{
			Appd:        appdV4,
			ABCIVersion: abci.ABCIClientVersion2,
			AppVersion:  4,
			StartArgs:   defaultArgs,
		}, abci.Version{
			Appd:        appdV5,
			ABCIVersion: abci.ABCIClientVersion2,
			AppVersion:  5,
			StartArgs:   defaultArgs,
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
		app.NodeHome,
		NewAppServer,
		appExporter,
		server.StartCmdOptions{
			AddFlags:            addStartFlags,
			StartCommandHandler: multiplexer.New(versions),
		},
	)
}
