package main

import (
	"github.com/testground/sdk-go/run"
)

var testcases = map[string]interface{}{}

func main() {
	run.InvokeMap(testcases)
}
