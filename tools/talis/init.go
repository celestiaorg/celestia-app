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

	"github.com/celestiaorg/celestia-app/v4/app"
	cmtconfig "github.com/cometbft/cometbft/config"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/spf13/cobra"
)

const (
	EnvVarSSHKeyName        = "TALIS_SSH_KEY_NAME"
	EnvVarPubSSHKeyPath     = "TALIS_SSH_PUB_KEY_PATH"
	EnvVarSSHKeyPath        = "TALIS_SSH_KEY_PATH"
	EnvVarDigitalOceanToken = "DIGITALOCEAN_TOKEN"
)

func initCmd() *cobra.Command {
	var (
		rootDir       string
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

			if err := CopyTalisScripts(rootDir); err != nil {
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
			consConfig := DefaultConfigProfile(cmtconfig.DefaultConfig(), tables)
			cmtconfig.WriteConfigFile(filepath.Join(rootDir, "config.toml"), consConfig)

			// the sdk requires a global template be set just to save a toml file without panicking
			serverconfig.SetConfigTemplate(serverconfig.DefaultConfigTemplate)
			serverconfig.WriteConfigFile(filepath.Join(rootDir, "app.toml"), app.DefaultAppConfig())

			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&chainID, "chainID", "c", "", "Chain ID (required)")
	cmd.MarkFlagRequired("chainID")
	cmd.Flags().StringVarP(&experiment, "experiment", "e", "test", "the name of the experiment (required)")
	cmd.MarkFlagRequired("experiment")
	cmd.Flags().StringArrayVarP(&tables, "tables", "t", []string{"consensus_round_state", "consensus_block"}, "the traces that will be collected")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get user home directory: %v", err)
	}
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
	cfg.P2P.SendRate = 100000000
	cfg.P2P.RecvRate = 110000000
	cfg.RPC.ListenAddress = "tcp://0.0.0.0:26657"
	cfg.RPC.GRPCListenAddress = "tcp://0.0.0.0:9090"
	return cfg
}

func initDirs(rootDir string) error {
	// 1) create the sub‑directories
	for _, d := range []string{"payload", "data", "scripts"} {
		dir := filepath.Join(rootDir, d)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}

	return nil
}

// CopyTalisScripts ensures that the celestia-app tools/talis/scripts directory
// is copied into destDir. It first checks GOPATH/src/github.com/.../scripts,
// and if missing, does a shallow git clone, copies the folder (including subdirectories), then cleans up.
func CopyTalisScripts(destDir string) error {
	const importPath = "github.com/celestiaorg/celestia-app/tools/talis/scripts"

	// 1) figure out GOPATH
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		out, err := exec.Command("go", "env", "GOPATH").Output()
		if err != nil {
			return fmt.Errorf("could not determine GOPATH: %w", err)
		}
		gopath = strings.TrimSpace(string(out))
	}

	// 2) local path where scripts should live
	src := filepath.Join(gopath, "src", importPath)

	// 3) if not present, clone repo to temp
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

	// 4) copy directory tree including subdirectories
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
			if err := os.MkdirAll(target, 0755); err != nil {
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
	if err := os.MkdirAll(destDir, 0755); err != nil {
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
