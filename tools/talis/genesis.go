package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/test/util/genesis"
	"github.com/spf13/cobra"
)

const (
	chainIDFlag = "chainID"
	rootDirFlag = "directory"
)

// generateCmd is the Cobra command for creating the payload for the experiment.
func generateCmd() *cobra.Command {
	var (
		rootDir                       string
		chainID                       string // will overwrite that in the config
		squareSize                    int
		appBinaryPath                 string
		nodeBinaryPath                string
		txsimBinaryPath               string
		useMainnetStakingDistribution bool
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

			err = createPayload(cfg.Validators, cfg.ChainID, payloadDir, squareSize, useMainnetStakingDistribution)
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
	cmd.Flags().IntVarP(&squareSize, "ods-size", "s", appconsts.SquareSizeUpperBound, "The size of the ODS for the network (make sure to also build a celestia-app binary with a greater SquareSizeUpperBound)")
	cmd.Flags().StringVarP(&appBinaryPath, "app-binary", "a", filepath.Join(gopath, "celestia-appd"), "app binary to include in the payload (assumes the binary is installed")
	cmd.Flags().StringVarP(&nodeBinaryPath, "node-binary", "n", filepath.Join(gopath, "celestia"), "node binary to include in the payload (assumes the binary is installed")
	cmd.Flags().StringVarP(&txsimBinaryPath, "txsim-binary", "t", filepath.Join(gopath, "txsim"), "txsim binary to include in the payload (assumes the binary is installed)")
	cmd.Flags().BoolVarP(&useMainnetStakingDistribution, "mainnet-staking-distribution", "m", false, "replace the default uniform staking distribution with the actual mainnet distribution")

	return cmd
}

// createPayload takes ips created by pulumi and the path to the payload directory
// to create the payload required for the experiment.
func createPayload(ips []Instance, chainID, ppath string, squareSize int, useMainnetDistribution bool, mods ...genesis.Modifier) error {
	n, err := NewNetwork(chainID, squareSize, mods...)
	if err != nil {
		return err
	}

	stake := int64(genesis.DefaultInitialBalance) / 2
	for index, info := range ips {
		if useMainnetDistribution {
			stake = getMainnetStake(index)
		}
		err = n.AddValidator(
			info.Name,
			info.PublicIP,
			ppath,
			info.Region,
			stake,
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

// mainnetVotingPowers contains the current Celestia mainnet staking distribution for more realistic tests.
var mainnetVotingPowers []int

func getMainnetStake(index int) int64 {
	if index < 0 {
		return 0
	}
	if len(mainnetVotingPowers) == 0 {
		// these figures reflect the exact staking values on 09/07/25.
		mainnetVotingPowers = []int{
			44706511, 44437002, 37932228, 37544929, 29421912, 27045838, 25722376, 25574864, 19573478, 17083572,
			14156979, 10990505, 10228508, 8017107, 7985256, 7465738, 7156557, 7000454, 6957695, 6816721,
			6497714, 6133878, 6061770, 6023778, 5837045, 5817421, 5788259, 5571126, 5504182, 5500773,
			5070168, 4672609, 4360060, 4326293, 3978439, 3894538, 3746172, 3608145, 3606324, 3606128,
			3600486, 3560552, 3538637, 3456887, 3449504, 3365860, 3330140, 3329077, 3242441, 3231836,
			3163103, 3162476, 3139329, 3132732, 3117200, 3071253, 3059325, 3043103, 3039694, 3038574,
			3038322, 3025332, 3025137, 3013047, 3011854, 3010337, 3004185, 3001607, 3000732, 3000592,
			3000433, 3000236, 3000215, 3000207, 3000142, 3000128, 3000126, 2689474, 2500012, 2329666,
			2242943, 2083890, 2038490, 1957574, 1619120, 1615290, 1482045, 1291544, 1286175, 1204480,
			1202416, 1156152, 1137365, 1101315, 1045017, 1000381, 977562, 948538, 820448, 445353,
		}
	}
	if index >= len(mainnetVotingPowers) {
		return int64(mainnetVotingPowers[len(mainnetVotingPowers)-1])
	}
	return int64(mainnetVotingPowers[index])
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
