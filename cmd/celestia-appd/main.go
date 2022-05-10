package main

import (
	"os"

	"github.com/celestiaorg/celestia-app/app"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
)

func main() {
	rootCmd := NewRootCmd()
	if err := svrcmd.Execute(rootCmd, "CELESTIA", app.DefaultNodeHome); err != nil {
		os.Exit(1)
	}
}
