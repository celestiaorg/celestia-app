//go:build multiplexer

package cmd

import (
	"fmt"
	"github.com/01builders/nova"
	"github.com/01builders/nova/abci"
	"github.com/01builders/nova/appd"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"runtime"

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
		panic(fmt.Errorf("failed to create v3 app for platform %s: %w", platform(), err))
	}

	versions, err := abci.NewVersions(abci.Version{
		Appd:        v3,
		ABCIVersion: abci.ABCIClientVersion1,
		AppVersion:  3,
		StartArgs: []string{
			"--grpc.enable=true",
			"--api.enable=true",
			"--api.swagger=false",
			"--with-tendermint=false",
			"--transport=grpc",
			"--v2-upgrade-height=3",
		},
	})
	if err != nil {
		panic(err)
	}

	rootCommand.AddCommand(
		nova.NewPassthroughCmd(versions),
	)

	// Add the following commands to the rootCommand: start, tendermint, export, version, and rollback and wire multiplexer.
	server.AddCommandsWithStartCmdOptions(
		rootCommand,
		app.DefaultNodeHome,
		NewAppServer,
		appExporter,
		server.StartCmdOptions{
			AddFlags:            addStartFlags,
			StartCommandHandler: nova.New(versions),
		},
	)
}

func platform() string {
	return runtime.GOOS + "_" + runtime.GOARCH
}
