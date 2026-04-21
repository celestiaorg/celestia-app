package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v9/test/util/genesis"
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
		buildDirPath                  string
		appBinaryPath                 string
		nodeBinaryPath                string
		txsimBinaryPath               string
		latencyMonitorBinaryPath      string
		fibreBinaryPath               string
		fibreTxsimBinaryPath          string
		observabilityDirPath          string
		useMainnetStakingDistribution bool
		fibreAccounts                 int
		encoderFibreAccounts          int
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
			if err := os.RemoveAll(filepath.Join(rootDir, "encoder-payload")); err != nil {
				return fmt.Errorf("failed to remove old encoder-payload directory: %w", err)
			}

			err = createPayload(cfg.Validators, cfg.Encoders, cfg.ChainID, payloadDir, squareSize, useMainnetStakingDistribution, fibreAccounts, encoderFibreAccounts)
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

			buildDest := filepath.Join(payloadDir, "build")
			if buildDirPath != "" {
				info, err := os.Stat(buildDirPath)
				if err != nil {
					return fmt.Errorf("failed to stat build directory %q: %w", buildDirPath, err)
				}
				if !info.IsDir() {
					return fmt.Errorf("build path %q is not a directory", buildDirPath)
				}
				if err := copyDir(buildDirPath, buildDest); err != nil {
					return fmt.Errorf("failed to copy build directory: %w", err)
				}
			} else {
				if err := copyFile(appBinaryPath, filepath.Join(buildDest, "celestia-appd"), 0o755); err != nil {
					return fmt.Errorf("failed to copy app binary: %w", err)
				}

				if err := copyFile(nodeBinaryPath, filepath.Join(buildDest, "celestia"), 0o755); err != nil {
					log.Println("failed to copy celestia binary, bridge and light nodes will not be able to start")
				}

				if err := copyFile(txsimBinaryPath, filepath.Join(buildDest, "txsim"), 0o755); err != nil {
					return fmt.Errorf("failed to copy txsim binary: %w", err)
				}

				// Copy latency monitor binary
				if err := copyFile(latencyMonitorBinaryPath, filepath.Join(buildDest, "latency-monitor"), 0o755); err != nil {
					log.Printf("failed to copy latency monitor binary: %v", err)
				}

				// Copy fibre server binary
				if err := copyFile(fibreBinaryPath, filepath.Join(buildDest, "fibre"), 0o755); err != nil {
					log.Printf("failed to copy fibre binary: %v", err)
				}

				// Copy fibre-txsim binary
				if err := copyFile(fibreTxsimBinaryPath, filepath.Join(buildDest, "fibre-txsim"), 0o755); err != nil {
					log.Printf("failed to copy fibre-txsim binary: %v", err)
				}
			}

			if err := writeAWSEnv(filepath.Join(payloadDir, "vars.sh"), cfg); err != nil {
				return fmt.Errorf("failed to write aws env: %w", err)
			}

			if err := stageObservabilityPayload(cfg, observabilityDirPath, payloadDir); err != nil {
				return fmt.Errorf("failed to stage observability payload: %w", err)
			}

			// Stage encoder payload: copy binaries, genesis, and vars to the
			// encoder-payload directory so deploy can create a lightweight tar.
			if len(cfg.Encoders) > 0 {
				if err := stageEncoderPayload(rootDir, payloadDir, appBinaryPath, fibreTxsimBinaryPath, buildDirPath); err != nil {
					return fmt.Errorf("failed to stage encoder payload: %w", err)
				}
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
	cmd.Flags().StringVarP(&buildDirPath, "build-dir", "b", "", "directory containing binaries to include in the payload")
	cmd.Flags().StringVarP(&appBinaryPath, "app-binary", "a", filepath.Join(gopath, "celestia-appd"), "app binary to include in the payload (assumes the binary is installed")
	cmd.Flags().StringVarP(&nodeBinaryPath, "node-binary", "n", filepath.Join(gopath, "celestia"), "node binary to include in the payload (assumes the binary is installed")
	cmd.Flags().StringVarP(&txsimBinaryPath, "txsim-binary", "t", filepath.Join(gopath, "txsim"), "txsim binary to include in the payload (assumes the binary is installed)")
	cmd.Flags().StringVar(&latencyMonitorBinaryPath, "latency-monitor-binary", filepath.Join(gopath, "latency-monitor"), "latency monitor binary to include in the payload")
	cmd.Flags().StringVar(&fibreBinaryPath, "fibre-binary", filepath.Join(gopath, "fibre"), "fibre server binary to include in the payload")
	cmd.Flags().StringVar(&fibreTxsimBinaryPath, "fibre-txsim-binary", filepath.Join(gopath, "fibre-txsim"), "fibre-txsim binary to include in the payload")
	cmd.Flags().StringVar(&observabilityDirPath, "observability-dir", "", "path to observability directory containing docker-compose, Prometheus config, and scripts (required if observability nodes are configured)")
	cmd.Flags().BoolVarP(&useMainnetStakingDistribution, "mainnet-staking-distribution", "m", false, "replace the default uniform staking distribution with the actual mainnet distribution")
	cmd.Flags().IntVar(&fibreAccounts, "fibre-accounts", 100, "number of pre-funded fibre accounts to create per validator")
	cmd.Flags().IntVar(&encoderFibreAccounts, "encoder-fibre-accounts", 100, "number of pre-funded fibre accounts to create per encoder instance")

	return cmd
}

// createPayload takes ips created by pulumi and the path to the payload directory
// to create the payload required for the experiment.
func createPayload(ips, encoders []Instance, chainID, ppath string, squareSize int, useMainnetDistribution bool, fibreAccounts, encoderFibreAccounts int, mods ...genesis.Modifier) error {
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
			fibreAccounts,
		)
		if err != nil {
			return err
		}
	}

	// Create encoder-payload directory and keyrings for each encoder.
	// Encoder keyrings are stored in <ppath>/../encoder-payload/<encoder-name>/
	// so that a separate, lighter tar can be built during deploy.
	encoderPayloadDir := filepath.Join(filepath.Dir(ppath), "encoder-payload")
	if len(encoders) > 0 {
		if err := os.MkdirAll(encoderPayloadDir, 0o755); err != nil {
			return fmt.Errorf("failed to create encoder-payload dir: %w", err)
		}
	}
	for _, enc := range encoders {
		if err := n.AddEncoder(enc.Name, encoderPayloadDir, encoderFibreAccounts); err != nil {
			return fmt.Errorf("failed to add encoder %s: %w", enc.Name, err)
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

// stageEncoderPayload copies the binaries (celestia-appd, fibre-txsim), genesis,
// vars.sh, and an encoder_init.sh script into the encoder-payload directory so
// that the deploy step can create a lightweight tar for encoder instances.
func stageEncoderPayload(rootDir, payloadDir, appBinaryPath, fibreTxsimBinaryPath, buildDirPath string) error {
	encPayload := filepath.Join(rootDir, "encoder-payload")

	// Build directory with only the two binaries an encoder needs
	encBuild := filepath.Join(encPayload, "build")
	if err := os.MkdirAll(encBuild, 0o755); err != nil {
		return err
	}

	if buildDirPath != "" {
		for _, name := range []string{"celestia-appd", "fibre-txsim"} {
			src := filepath.Join(buildDirPath, name)
			if err := copyFile(src, filepath.Join(encBuild, name), 0o755); err != nil {
				return fmt.Errorf("copy %s from build dir: %w", name, err)
			}
		}
	} else {
		if err := copyFile(appBinaryPath, filepath.Join(encBuild, "celestia-appd"), 0o755); err != nil {
			return fmt.Errorf("copy celestia-appd: %w", err)
		}
		if err := copyFile(fibreTxsimBinaryPath, filepath.Join(encBuild, "fibre-txsim"), 0o755); err != nil {
			return fmt.Errorf("copy fibre-txsim: %w", err)
		}
	}

	// Copy genesis and vars.sh
	if err := copyFile(filepath.Join(payloadDir, "genesis.json"), filepath.Join(encPayload, "genesis.json"), 0o644); err != nil {
		return fmt.Errorf("copy genesis.json: %w", err)
	}
	if err := copyFile(filepath.Join(payloadDir, "vars.sh"), filepath.Join(encPayload, "vars.sh"), 0o755); err != nil {
		return fmt.Errorf("copy vars.sh: %w", err)
	}

	// Write the encoder init script
	return writeEncoderInitScript(filepath.Join(encPayload, "encoder_init.sh"))
}

// writeEncoderInitScript creates a minimal init script for encoder instances.
// Encoders only need the fibre-txsim binary, celestia-appd (for escrow deposits),
// a keyring, and genesis.
func writeEncoderInitScript(path string) error {
	script := `#!/bin/bash
set -euo pipefail

# On AWS i-family instances the local NVMe is mounted at /mnt/data by
# cloud-init (see tools/talis/aws.go). Put celestia-app state there so
# fibre-txsim keyring reads go to NVMe and match the validator layout.
# DO and AWS sizes without local NVMe have no /mnt/data; fall through
# to $HOME.
STATE_BASE="$HOME"
if mountpoint -q /mnt/data 2>/dev/null; then
  STATE_BASE="/mnt/data"
fi
CELES_HOME="$STATE_BASE/.celestia-app"

export DEBIAN_FRONTEND=noninteractive
apt-get update -y -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold"
apt-get install curl jq chrony --yes -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold"

systemctl enable chrony
systemctl start chrony

# TCP BBR
modprobe tcp_bbr || true
sysctl -w net.core.default_qdisc=fq
sysctl -w net.ipv4.tcp_congestion_control=bbr

# Install binaries
cp encoder-payload/build/celestia-appd /bin/celestia-appd
cp encoder-payload/build/fibre-txsim /bin/fibre-txsim

source encoder-payload/vars.sh

# Determine this encoder's directory from hostname (e.g. "encoder-0")
hostname=$(hostname)
parsed_hostname=$(echo "$hostname" | awk -F'-' '{print $1 "-" $2}')

# Set up celestia-app home with keyring + genesis
rm -rf "$CELES_HOME"
mkdir -p "$CELES_HOME/config"
cp encoder-payload/genesis.json "$CELES_HOME/config/genesis.json"
cp -r "encoder-payload/$parsed_hostname/keyring-test" "$CELES_HOME/"

echo "Encoder $parsed_hostname initialized"
`
	return os.WriteFile(path, []byte(script), 0o755)
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
