package main

import (
	"os"

	"github.com/celestiaorg/celestia-app/cmd/celestia-appd/cmd"
)

func main() {
	rootCmd, _ := cmd.NewRootCmd()
	if err := cmd.Execute(rootCmd); err != nil {
		os.Exit(1)
	}
}
