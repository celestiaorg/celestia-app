//go:build multiplexer

package cmd

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/app"
	embedding "github.com/celestiaorg/celestia-app/v10/internal/embedding"
	"github.com/celestiaorg/celestia-app/v10/multiplexer/abci"
	"github.com/celestiaorg/celestia-app/v10/multiplexer/appd"
	multiplexer "github.com/celestiaorg/celestia-app/v10/multiplexer/cmd"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// v2UpgradeHeight is the block height at which the v2 upgrade occurred.
// this can be overridden at build time using ldflags:
// -ldflags="-X 'github.com/celestiaorg/celestia-app/v10/cmd/celestia-appd/cmd.v2UpgradeHeight=1751707'" for arabica
// -ldflags="-X 'github.com/celestiaorg/celestia-app/v10/cmd/celestia-appd/cmd.v2UpgradeHeight=2585031'" for mocha
// -ldflags="-X 'github.com/celestiaorg/celestia-app/v10/cmd/celestia-appd/cmd.v2UpgradeHeight=2371495'" for mainnet
var v2UpgradeHeight = ""

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

	v6Tag, v6CompressedBinary, err := embedding.CelestiaAppV6()
	if err != nil {
		panic(err)
	}

	appdV6, err := appd.New(v6Tag, v6CompressedBinary)
	if err != nil {
		panic(err)
	}

	v7Tag, v7CompressedBinary, err := embedding.CelestiaAppV7()
	if err != nil {
		panic(err)
	}

	appdV7, err := appd.New(v7Tag, v7CompressedBinary)
	if err != nil {
		panic(err)
	}

	v8Tag, v8CompressedBinary, err := embedding.CelestiaAppV8()
	if err != nil {
		panic(err)
	}

	appdV8, err := appd.New(v8Tag, v8CompressedBinary)
	if err != nil {
		panic(err)
	}

	v9Tag, v9CompressedBinary, err := embedding.CelestiaAppV9()
	if err != nil {
		panic(err)
	}

	appdV9, err := appd.New(v9Tag, v9CompressedBinary)
	if err != nil {
		panic(err)
	}

	v3Args := defaultArgs
	if v2UpgradeHeight != "" && v2UpgradeHeight != "0" {
		v3Args = append(v3Args, "--v2-upgrade-height="+v2UpgradeHeight)
	}

	// v4 and v5 hard-fail on startup if minimum-gas-prices is empty (their
	// cosmos-sdk fork requires it), unlike v3 (allowed empty) and v6+ (fall
	// back to a hardcoded default). Pin the era's default so nodes syncing
	// from genesis with an old app.toml don't get stuck at the v3->v4
	// upgrade height. The value only affects local mempool filtering while
	// replaying historical blocks, so overriding is harmless.
	legacyMinGasPricesArgs := append([]string{
		fmt.Sprintf("--minimum-gas-prices=%v%s", appconsts.LegacyDefaultMinGasPrice, appconsts.BondDenom),
	}, defaultArgs...)

	versions, err := abci.NewVersions(
		abci.Version{
			Appd:        appdV3,
			ABCIVersion: abci.ABCIClientVersion1,
			AppVersion:  3,
			StartArgs:   v3Args,
		}, abci.Version{
			Appd:        appdV4,
			ABCIVersion: abci.ABCIClientVersion2,
			AppVersion:  4,
			StartArgs:   legacyMinGasPricesArgs,
		}, abci.Version{
			Appd:        appdV5,
			ABCIVersion: abci.ABCIClientVersion2,
			AppVersion:  5,
			StartArgs:   legacyMinGasPricesArgs,
		}, abci.Version{
			Appd:        appdV6,
			ABCIVersion: abci.ABCIClientVersion2,
			AppVersion:  6,
			StartArgs:   defaultArgs,
		}, abci.Version{
			Appd:        appdV7,
			ABCIVersion: abci.ABCIClientVersion2,
			AppVersion:  7,
			StartArgs:   defaultArgs,
		}, abci.Version{
			Appd:        appdV8,
			ABCIVersion: abci.ABCIClientVersion2,
			AppVersion:  8,
			StartArgs:   defaultArgs,
		}, abci.Version{
			Appd:        appdV9,
			ABCIVersion: abci.ABCIClientVersion2,
			AppVersion:  9,
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
