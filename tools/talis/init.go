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
// is copied into destDir. It tries multiple strategies to find the scripts.
func CopyTalisScripts(destDir string) error {
	var candidatePaths []string

	// 1) Try current working directory first (most common case)
	if cwd, err := os.Getwd(); err == nil {
		candidatePaths = append(candidatePaths, filepath.Join(cwd, "tools", "talis", "scripts"))
	}

	// 2) Try relative to the binary location
	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		// If running from celestia-app/tools/talis/
		candidatePaths = append(candidatePaths, filepath.Join(execDir, "scripts"))
		// If running from celestia-app/build/
		candidatePaths = append(candidatePaths, filepath.Join(execDir, "..", "tools", "talis", "scripts"))
		// If running from celestia-app/
		candidatePaths = append(candidatePaths, filepath.Join(execDir, "tools", "talis", "scripts"))

		// Follow symlinks if necessary
		if realPath, err := filepath.EvalSymlinks(execPath); err == nil && realPath != execPath {
			realDir := filepath.Dir(realPath)
			candidatePaths = append(candidatePaths, filepath.Join(realDir, "scripts"))
			candidatePaths = append(candidatePaths, filepath.Join(realDir, "..", "tools", "talis", "scripts"))
			candidatePaths = append(candidatePaths, filepath.Join(realDir, "tools", "talis", "scripts"))
		}
	}

	// 3) Try go.mod root directory
	if cwd, err := os.Getwd(); err == nil {
		if modRoot := findModuleRoot(cwd); modRoot != "" {
			candidatePaths = append(candidatePaths, filepath.Join(modRoot, "tools", "talis", "scripts"))
		}
	}

	// 4) Try looking for celestia-app directory in common locations
	if homeDir, err := os.UserHomeDir(); err == nil {
		candidatePaths = append(candidatePaths,
			filepath.Join(homeDir, "git", "celestia-app", "tools", "talis", "scripts"),
			filepath.Join(homeDir, "go", "src", "github.com", "celestiaorg", "celestia-app", "tools", "talis", "scripts"),
		)
	}

	// Try all candidate paths
	for _, src := range candidatePaths {
		if fi, err := os.Stat(src); err == nil && fi.IsDir() {
			fmt.Printf("Found scripts at: %s\n", src)
			return copyDir(src, filepath.Join(destDir, "scripts"))
		}
	}

	// 5) Last resort: try GOPATH (for backwards compatibility)
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		if out, err := exec.Command("go", "env", "GOPATH").Output(); err == nil {
			gopath = strings.TrimSpace(string(out))
		}
	}

	if gopath != "" {
		src := filepath.Join(gopath, "src", "github.com", "celestiaorg", "celestia-app", "tools", "talis", "scripts")
		if fi, err := os.Stat(src); err == nil && fi.IsDir() {
			return copyDir(src, filepath.Join(destDir, "scripts"))
		}
	}

	// 6) Final fallback: clone repo to temp (but clone the current branch if possible)
	return cloneAndCopyScripts(destDir)
}

// cloneAndCopyScripts clones the repository and copies scripts
func cloneAndCopyScripts(destDir string) error {
	tmp, err := os.MkdirTemp("", "celestia-scripts-*")
	if err != nil {
		return fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(tmp)

	repo := "https://github.com/celestiaorg/celestia-app.git"

	// Try to determine current branch if we're in a git repo
	branch := "main"
	if cwd, err := os.Getwd(); err == nil {
		if gitRoot := findGitRoot(cwd); gitRoot != "" {
			if currentBranch, err := exec.Command("git", "-C", gitRoot, "branch", "--show-current").Output(); err == nil {
				if b := strings.TrimSpace(string(currentBranch)); b != "" {
					branch = b
					fmt.Printf("Attempting to clone branch: %s\n", branch)
				}
			}
		}
	}

	// Try cloning the specific branch first
	cmd := exec.Command("git", "clone", "--depth=1", "--branch", branch, repo, tmp)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to clone branch %s, trying main branch\n", branch)
		// Fallback to main branch
		os.RemoveAll(tmp) // Clean up failed attempt
		tmp, err = os.MkdirTemp("", "celestia-scripts-*")
		if err != nil {
			return fmt.Errorf("mktemp: %w", err)
		}

		cmd = exec.Command("git", "clone", "--depth=1", repo, tmp)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git clone failed: %w", err)
		}
	}

	src := filepath.Join(tmp, "tools", "talis", "scripts")
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("scripts directory not found in cloned repository at %s", src)
	}

	return copyDir(src, filepath.Join(destDir, "scripts"))
}

// findGitRoot searches upward from the given directory for .git
func findGitRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached root
		}
		dir = parent
	}
	return ""
}

// findModuleRoot searches upward from the given directory for go.mod
func findModuleRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached root
		}
		dir = parent
	}
	return ""
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
