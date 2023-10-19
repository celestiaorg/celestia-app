package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/p2p/pex"
)

const (
	// inputFile is the filename of the input file containing the list of peers.
	inputFile = "peers.txt"

	// outputFile is the filename of the output file that will be generated.
	outputFile = "addrbook.json"

	// routabilityStrict is a hard-coded config value for the address book.
	// See https://github.com/celestiaorg/celestia-core/blob/793ece9bbd732aec3e09018e37dc31f4bfe122d9/config/config.go#L540-L542
	routabilityStrict = true
)

func main() {
	data, err := os.ReadFile(inputFile)
	if err != nil {
		panic(err)
	}
	lines := strings.Split(string(data), "\n")

	book := pex.NewAddrBook(outputFile, routabilityStrict)
	for _, line := range lines {
		if line == "" {
			continue
		}
		address, err := p2p.NewNetAddressString(line)
		if err != nil {
			panic(err)
		}
		err = book.AddAddress(address, address)
		if err != nil {
			panic(err)
		}
	}

	book.Save()
	fmt.Printf("Converted %s into %s\n", inputFile, outputFile)
}
