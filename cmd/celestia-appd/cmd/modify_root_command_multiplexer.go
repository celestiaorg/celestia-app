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

// v2UpgradeHeight is the block height at which the v2 upgrade occurred.
// this can be overridden at build time using ldflags:
// -ldflags="-X 'github.com/celestiaorg/celestia-app/v4/cmd/celestia-appd/cmd.v2UpgradeHeight=1751707'" for arabica
// -ldflags="-X 'github.com/celestiaorg/celestia-app/v4/cmd/celestia-appd/cmd.v2UpgradeHeight=2585031'" for mocha
// -ldflags="-X 'github.com/celestiaorg/celestia-app/v4/cmd/celestia-appd/cmd.v2UpgradeHeight=2371495'" for mainnet
var v2UpgradeHeight = ""

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

	var extraArgs []string
	if v2UpgradeHeight != "" {
		extraArgs = append(extraArgs, "--v2-upgrade-height="+v2UpgradeHeight)
	}

	versions, err := abci.NewVersions(abci.Version{
		Appd:        v3,
		ABCIVersion: abci.ABCIClientVersion1,
		AppVersion:  3,
		StartArgs: append([]string{
			"--grpc.enable=true",
			"--api.enable=true",
			"--api.swagger=false",
			"--with-tendermint=false",
			"--transport=grpc",
		}, extraArgs...),
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
