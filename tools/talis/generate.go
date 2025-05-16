package main

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
)

const (
	chainIDFlag    = "chainID"
	payloadFlag    = "payload"
	validatorsFlag = "validators"
)

// generateCmd is the Cobra command for creating the payload for the experiment.
func generateCmd() *cobra.Command {
	payloadCmd := &cobra.Command{
		Use:   "generate",
		Short: "Create the payload for the experiment",
		RunE: func(cmd *cobra.Command, args []string) error {
			chainID, ppath, vpath := readFlags(cmd)
			ips, err := ReadValidatorsFromFile(vpath)
			if err != nil {
				return err
			}
			err = createPayload(ips, chainID, ppath)
			if err != nil {
				log.Fatalf("Failed to create payload: %v", err)
			}

			return nil
		},
	}

	// Flags for the payload command
	payloadCmd.Flags().StringP(chainIDFlag, "c", "test", "Chain ID (required)")
	payloadCmd.MarkFlagRequired(chainIDFlag)
	payloadCmd.Flags().StringP(payloadFlag, "p", "./payload", "Path to the payload directory (required)")
	payloadCmd.MarkFlagRequired(payloadFlag)
	payloadCmd.Flags().StringP(validatorsFlag, "v", "./payload/validators.json", "Path to the validators file (required)")
	payloadCmd.MarkFlagRequired(validatorsFlag)
	return payloadCmd
}

func readFlags(cmd *cobra.Command) (chainID, ppath, vpath string) {
	chainID, err := cmd.Flags().GetString(chainIDFlag)
	if err != nil {
		log.Fatalf("Failed to read chainID flag: %v", err)
	}
	ppath, err = cmd.Flags().GetString(payloadFlag)
	if err != nil {
		log.Fatalf("Failed to read payload flag: %v", err)
	}
	vpath, err = cmd.Flags().GetString(validatorsFlag)
	if err != nil {
		log.Fatalf("Failed to read validators flag: %v", err)
	}
	return chainID, ppath, vpath
}

// createPayload takes ips created by pulumi the path to the payload directory
// to create the payload required for the experiment.
func createPayload(ips map[string]NodeInfo, chainID, ppath string) error {
	n, err := NewNetwork(chainID)
	if err != nil {
		return err
	}

	for _, info := range ips {
		n.AddValidator(
			info.Name,
			info.Region,
			info.IP,
			ppath,
		)
	}

	for _, val := range n.genesis.Validators() {
		fmt.Println(val.Name, val.ConsensusKey.PubKey())
	}

	err = n.InitNodes(ppath)
	if err != nil {
		return err
	}

	err = n.SaveAddressBook(ppath, n.Peers())
	if err != nil {
		return err
	}

	return nil

}
