package main

import (
	"context"
	"fmt"
	"os"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	if err := newRootCmd().ExecuteContext(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "fibre: %v\n", err)
		os.Exit(1)
	}
}
