package main

import (
	"github.com/celestiaorg/celestia-app/test/testground/network"
	"github.com/testground/sdk-go/run"
)

var testcases = map[string]interface{}{
	"entrypoint": network.EntryPoint,
}

func main() {
	run.InvokeMap(testcases)
}
