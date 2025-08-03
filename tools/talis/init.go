package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/celestiaorg/celestia-app/v6/app"
	cmtconfig "github.com/cometbft/cometbft/config"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/spf13/cobra"
)

const (
	EnvVarSSHKeyName         = "TALIS_SSH_KEY_NAME"
	EnvVarPubSSHKeyPath      = "TALIS_SSH_PUB_KEY_PATH"
	EnvVarSSHKeyPath         = "TALIS_SSH_KEY_PATH"
	EnvVarDigitalOceanToken  = "DIGITALOCEAN_TOKEN"
	EnvVarAWSAccessKeyID     = "AWS_ACCESS_KEY_ID"
	EnvVarAWSSecretAccessKey = "AWS_SECRET_ACCESS_KEY"
	EnvVarAWSRegion          = "AWS_DEFAULT_REGION"
	EnvVarS3Bucket           = "AWS_S3_BUCKET"
	EnvVarS3Endpoint         = "AWS_S3_ENDPOINT"
	EnvVarChainID            = "CHAIN_ID"
	mebibyte                 = 1_048_576
)

func initCmd() *cobra.Command {
	var (
		rootDir       string
		srcRoot       string
		chainID       string
		experiment    string
		SSHPubKeyPath string
		SSHKeyName    string
		tables        []string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the Talis network",
		Long:  "Initialize the Talis network with the provided configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initDirs(rootDir); err != nil {
				return fmt.Errorf("failed to initialize directories: %w", err)
			}

			if err := CopyTalisScripts(rootDir, srcRoot); err != nil {
				return fmt.Errorf("failed to copy scripts: %w", err)
			}

			// todo: use the number of validators, bridges, and lights to create the config
			cfg := NewConfig(experiment, chainID).
				WithSSHPubKeyPath(SSHPubKeyPath).
				WithSSHKeyName(SSHKeyName)

			if err := cfg.Save(rootDir); err != nil {
				return fmt.Errorf("failed to save init config: %w", err)
			}

			// write the default config files that will be copied to the payload
			// for each validator unless otherwise overridden
			consensusConfig := app.DefaultConsensusConfig()
			consConfig := DefaultConfigProfile(consensusConfig, tables)
			cmtconfig.WriteConfigFile(filepath.Join(rootDir, "config.toml"), consConfig)

			// the sdk requires a global template be set just to save a toml file without panicking
			serverconfig.SetConfigTemplate(serverconfig.DefaultConfigTemplate)

			appconfig := app.DefaultAppConfig()
			appconfig.GRPC.Enable = true
			appconfig.GRPC.Address = "0.0.0.0:9091"
			serverconfig.WriteConfigFile(filepath.Join(rootDir, "app.toml"), appconfig)

			return nil
		},
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get user home directory: %v", err)
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&srcRoot, "src-root", "r", homeDir, "directory from which to copy scripts") // todo: fix the default directory here
	cmd.Flags().StringVarP(&chainID, "chainID", "c", "", "Chain ID (required)")
	_ = cmd.MarkFlagRequired("chainID")
	cmd.Flags().StringVarP(&experiment, "experiment", "e", "test", "the name of the experiment (required)")
	_ = cmd.MarkFlagRequired("experiment")
	cmd.Flags().StringArrayVarP(&tables, "tables", "t", []string{"consensus_round_state", "consensus_block", "mempool_tx"}, "the traces that will be collected")

	defaultKeyPath := filepath.Join(homeDir, ".ssh", "id_ed25519.pub")
	cmd.Flags().StringVarP(&SSHPubKeyPath, "ssh-pub-key-path", "s", defaultKeyPath, "path to the user's SSH public key")

	user, err := user.Current()
	if err != nil {
		log.Fatalf("failed to get current user: %v", err)
	}
	defaultKeyName := user.Username
	cmd.Flags().StringVarP(&SSHKeyName, "ssh-key-name", "n", defaultKeyName, "name for the SSH key")

	return cmd
}

func DefaultConfigProfile(cfg *cmtconfig.Config, tables []string) *cmtconfig.Config {
	cfg.Instrumentation.TracingTables = strings.Join(tables, ",")
	cfg.Instrumentation.TraceType = "local"
	cfg.P2P.SendRate = 100 * mebibyte
	cfg.P2P.RecvRate = 110 * mebibyte
	cfg.RPC.ListenAddress = "tcp://0.0.0.0:26657"
	cfg.RPC.GRPCListenAddress = "tcp://0.0.0.0:9090"
	return cfg
}

func initDirs(rootDir string) error {
	// 1) create the sub‑directories
	for _, d := range []string{"payload", "data", "scripts"} {
		dir := filepath.Join(rootDir, d)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}

	return nil
}

// CopyTalisScripts ensures that the celestia-app tools/talis/scripts directory
// is copied into destDir. It first checks GOPATH/src/github.com/.../scripts,
// and if missing, does a shallow git clone, copies the folder (including subdirectories), then cleans up.
func CopyTalisScripts(destDir string, srcRoot string) error {
	// todo: fix import path
	const importPath = "celestia-app/tools/talis/scripts"

	src := filepath.Join(srcRoot, "src", importPath)

	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		tmp, err := os.MkdirTemp("", "celestia-scripts-*")
		if err != nil {
			return fmt.Errorf("mktemp: %w", err)
		}
		defer os.RemoveAll(tmp)

		repo := "https://github.com/celestiaorg/celestia-app.git"
		cmd := exec.Command("git", "clone", "--depth=1", repo, tmp)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git clone failed: %w", err)
		}

		src = filepath.Join(tmp, "tools", "talis", "scripts")
	}

	// copy directory tree including subdirectories
	return copyDir(src, filepath.Join(destDir, "scripts"))
}

// copyDir recursively copies a directory tree, attempting to preserve permissions.
func copyDir(src string, dest string) error {
	// walk through source
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dest, rel)

		if info.IsDir() {
			// create directory
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			return nil
		}

		// it's a file; copy it
		return copyFile(path, target, info.Mode())
	})
}

// copyFile copies a single file from src to dest, preserving permissions and creating parent directories if needed.
func copyFile(srcFile, destFile string, perm os.FileMode) error {
	destDir := filepath.Dir(destFile)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("failed to create parent directory %s: %w", destDir, err)
	}

	src, err := os.Open(srcFile)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", srcFile, err)
	}
	defer src.Close()

	dest, err := os.OpenFile(destFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("failed to open destination file %s: %w", destFile, err)
	}
	defer dest.Close()

	if _, err = io.Copy(dest, src); err != nil {
		return fmt.Errorf("failed to copy data: %w", err)
	}

	return nil
}
