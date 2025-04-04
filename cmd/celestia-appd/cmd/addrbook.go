package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/celestiaorg/celestia-app/v3/app"          // Import Celestia app for default configuration
	"github.com/spf13/cobra"                              // CLI framework used to create commands
	"github.com/tendermint/tendermint/p2p"                // Handles peer-to-peer networking addresses
	"github.com/tendermint/tendermint/p2p/pex"            // Provides the address book (PEX) functionality
)

// addrbookCommand creates a CLI command to convert a peer list into an address book JSON file
func addrbookCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "addrbook peers.txt addrbook.json", // Command usage format
		Short: "Convert a list of peers into an address book", // Short description
		Long: "Convert a list of peers into an address book.\n" +
			"The first argument (peers.txt) should contain a new line separated list of peers. The format for a peer is `id@ip:port` or `id@domain:port`.\n" +
			"The second argument (addrbook.json) should contain the complete file path, including both the directory path and the desired output file name, in the following format: `path/to/filename`. The address book is saved to the output file in JSON format.\n",
		Args: cobra.ExactArgs(2), // Requires exactly two arguments: input and output files
		RunE: func(_ *cobra.Command, args []string) error {
			inputFile := args[0]  // The path to the peer list file
			outputFile := args[1] // The path to save the generated address book

			// Read the contents of the input peer list file
			data, err := os.ReadFile(inputFile)
			if err != nil {
				return err // Return error if file can't be read
			}

			// Split the file content by newline to get each peer address
			lines := strings.Split(string(data), "\n")

			// Load default configuration to determine strict address checking
			routabilityStrict := app.DefaultConsensusConfig().P2P.AddrBookStrict

			// Create a new address book with the given output path and strictness
			book := pex.NewAddrBook(outputFile, routabilityStrict)

			// Iterate through each peer address line
			for _, line := range lines {
				if line == "" {
					continue // Skip empty lines
				}

				// Parse the peer string into a NetAddress (id@ip:port)
				address, err := p2p.NewNetAddressString(line)
				if err != nil {
					fmt.Printf("Error parsing %s: %s\n", line, err)
					continue // Skip lines that fail to parse
				}

				// Add the peer address to the address book
				err = book.AddAddress(address, address)
				if err != nil {
					fmt.Printf("Error adding %s: %s\n", address, err)
					continue // Skip if unable to add address
				}
			}

			// Save the populated address book to the output file
			book.Save()
			fmt.Printf("Converted %s into %s\n", inputFile, outputFile)
			return nil
		},
	}

	return cmd // Return the configured CLI command
}
