package main

import (
	"os"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/cmd/celestia-appd/cmd"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
)

func main() {
	rootCmd := cmd.NewRootCmd()
	if err := svrcmd.Execute(rootCmd, cmd.EnvPrefix, app.DefaultNodeHome); err != nil {
		os.Exit(1)
	}
}
