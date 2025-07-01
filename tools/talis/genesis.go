package main

import (
	"fmt"
	"log"
	"os"
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
		rootDir         string
		chainID         string // will overwrite that in the config
		squareSize      int
		appBinaryPath   string
		nodeBinaryPath  string
		txsimBinaryPath string
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

			payloadDir := filepath.Join(rootDir, "payload")

			if err := os.RemoveAll(payloadDir); err != nil {
				return fmt.Errorf("failed to remove old payload directory: %w", err)
			}

			err = createPayload(cfg.Validators, cfg.ChainID, payloadDir, squareSize)
			if err != nil {
				log.Fatalf("Failed to create payload: %v", err)
			}

			srcCmtConfig := filepath.Join(rootDir, "config.toml")
			srcAppConfig := filepath.Join(rootDir, "app.toml")

			for _, v := range cfg.Validators {
				valDir := filepath.Join(payloadDir, v.Name)
				if err := copyFile(srcCmtConfig, filepath.Join(valDir, "config.toml"), 0o755); err != nil {
					return fmt.Errorf("failed to copy config.toml: %w", err)
				}

				if err := copyFile(srcAppConfig, filepath.Join(valDir, "app.toml"), 0o755); err != nil {
					return fmt.Errorf("failed to copy app.toml: %w", err)
				}
			}

			if err := copyDir(filepath.Join(rootDir, "scripts"), filepath.Join(rootDir, "payload")); err != nil {
				return fmt.Errorf("failed to copy scripts: %w", err)
			}

			if err := copyFile(appBinaryPath, filepath.Join(payloadDir, "build", "celestia-appd"), 0o755); err != nil {
				return fmt.Errorf("failed to copy app binary: %w", err)
			}

			if err := copyFile(nodeBinaryPath, filepath.Join(payloadDir, "build", "celestia"), 0o755); err != nil {
				log.Println("failed to copy celestia binary, bridge and light nodes will not be able to start")
			}

			if err := copyFile(txsimBinaryPath, filepath.Join(payloadDir, "build", "txsim"), 0o755); err != nil {
				return fmt.Errorf("failed to copy txsim binary: %w", err)
			}

			if err := writeAWSEnv(filepath.Join(payloadDir, "vars.sh"), cfg); err != nil {
				return fmt.Errorf("failed to write aws env: %w", err)
			}

			return cfg.Save(rootDir)
		},
	}

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			panic("failed to determine home dir: " + err.Error())
		}
		gopath = filepath.Join(home, "go")
	}
	gopath = filepath.Join(gopath, "bin")

	cmd.Flags().StringVarP(&chainID, chainIDFlag, "c", "", "Override the chainID in the config")
	cmd.Flags().StringVarP(&rootDir, rootDirFlag, "d", ".", "root directory in which to initialize (default is the current directory)")
	cmd.Flags().IntVarP(&squareSize, "ods-size", "s", appconsts.TalisSquareSizeUpperBound, "The size of the ODS for the network (make sure to also build a celestia-app binary with a greater SquareSizeUpperBound)")
	cmd.Flags().StringVarP(&appBinaryPath, "app-binary", "a", filepath.Join(gopath, "celestia-appd"), "app binary to include in the payload (assumes the binary is installed")
	cmd.Flags().StringVarP(&nodeBinaryPath, "node-binary", "n", filepath.Join(gopath, "celestia"), "node binary to include in the payload (assumes the binary is installed")
	cmd.Flags().StringVarP(&txsimBinaryPath, "txsim-binary", "t", filepath.Join(gopath, "txsim"), "txsim binary to include in the payload (assumes the binary is installed)")

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
		err = n.AddValidator(
			info.Name,
			info.PublicIP,
			ppath,
			info.Region,
		)
		if err != nil {
			return err
		}
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

func writeAWSEnv(varsPath string, cfg Config) error {
	f, err := os.OpenFile(varsPath,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o755,
	)
	if err != nil {
		return fmt.Errorf("failed to open vars.sh for append: %w", err)
	}
	defer f.Close()

	exports := []string{
		fmt.Sprintf("export AWS_DEFAULT_REGION=%q\n", cfg.S3Config.Region),
		fmt.Sprintf("export AWS_ACCESS_KEY_ID=%q\n", cfg.S3Config.AccessKeyID),
		fmt.Sprintf("export AWS_SECRET_ACCESS_KEY=%q\n", cfg.S3Config.SecretAccessKey),
		fmt.Sprintf("export AWS_S3_BUCKET=%q\n", cfg.S3Config.BucketName),
		fmt.Sprintf("export AWS_S3_ENDPOINT=%q\n", cfg.S3Config.Endpoint),
		fmt.Sprintf("export CHAIN_ID=%q\n", cfg.ChainID),
	}

	for _, line := range exports {
		if _, err := f.WriteString(line); err != nil {
			return fmt.Errorf("failed to append to vars.sh: %w", err)
		}
	}

	return nil
}
