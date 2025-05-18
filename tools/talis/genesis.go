package main

import (
	"fmt"
	"log"
	"path/filepath"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/test/util/genesis"
	"github.com/spf13/cobra"
)

const (
	chainIDFlag = "chainID"
	rootDirFlag = "directory"
)

// generateCmd is the Cobra command for creating the payload for the experiment.
func generateCmd() *cobra.Command {
	var (
		rootDir    string
		chainID    string // will overwrite that in the config
		squareSize int
	)
	cmd := &cobra.Command{
		Use:   "genesis",
		Short: "Create a genesis for the network.",
		Long:  "Create a genesis for the network along with everything else needed to start the network. Call this only after init and add.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if chainID != "" {
				cfg = cfg.WithChainID(chainID)
			}

			err = createPayload(cfg.Validators, cfg.ChainID, filepath.Join(rootDir, "payload"), squareSize)
			if err != nil {
				log.Fatalf("Failed to create payload: %v", err)
			}

			srcCmtConfig := filepath.Join(rootDir, "config.toml")
			srcAppConfig := filepath.Join(rootDir, "app.toml")

			for _, v := range cfg.Validators {
				valDir := filepath.Join(rootDir, "payload", v.Name)
				if err := copyFile(srcCmtConfig, filepath.Join(valDir, "config.toml"), 0644); err != nil {
					return fmt.Errorf("failed to copy config.toml: %w", err)
				}

				if err := copyFile(srcAppConfig, filepath.Join(valDir, "app.toml"), 0644); err != nil {
					return fmt.Errorf("failed to copy app.toml: %w", err)
				}

				if err := copyDir(filepath.Join(rootDir, "scripts"), valDir); err != nil {
					return fmt.Errorf("failed to copy scripts: %w", err)
				}
			}

			return cfg.Save(rootDir)
		},
	}

	// Flags for the payload command
	cmd.Flags().StringVarP(&chainID, chainIDFlag, "c", "", "Override the chainID in the config")
	cmd.Flags().StringVarP(&rootDir, rootDirFlag, "d", ".", "root directory in which to initialize (default is the current directory)")
	cmd.Flags().IntVarP(&squareSize, "ods-size", "s", appconsts.TalisSquareSizeUpperBound, "The size of the ODS for the network")

	return cmd
}

// createPayload takes ips created by pulumi the path to the payload directory
// to create the payload required for the experiment.
func createPayload(ips []Instance, chainID, ppath string, squareSize int, mods ...genesis.Modifier) error {
	n, err := NewNetwork(chainID, squareSize, mods...)
	if err != nil {
		return err
	}

	for _, info := range ips {
		n.AddValidator(
			info.Name,
			info.Region,
			info.PublicIP,
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
