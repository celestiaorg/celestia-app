package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	talisClient "github.com/celestiaorg/talis/pkg/api/v1/client"
	"github.com/celestiaorg/talis/pkg/db/models"
	"github.com/celestiaorg/talis/pkg/types"
	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	var (
		rootDir    string
		validators int
		chainID    string
		talisIP    string
		talisKey   string
		project    string
		userID     int
		userSSH    string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the Talis network",
		Long:  "Initialize the Talis network with the provided configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := PingTalisServer(talisIP, talisKey); err != nil {
				return fmt.Errorf("failed to ping Talis server with valid IP and Key: %w", err)
			}

			if err := initDirs(rootDir); err != nil {
				return fmt.Errorf("failed to initialize directories: %w", err)
			}

			if err := CopyTalisScripts(rootDir); err != nil {
				return fmt.Errorf("failed to copy scripts: %w", err)
			}

			cfg := NewTestConfig()
			cfg.ChainID = chainID
			cfg.IP = talisIP
			cfg.Key = talisKey
			cfg.Project = project
			cfg.UserID = userID
			cfg.UserSSHKeyName = userSSH
			// todo: switch to using random regions and specific numbers of
			// validators

			p, err := OpenProject(cfg, project)
			if err != nil {
				return err
			}

			// Talis will automatically rename the project without telling us so
			// we need to update it here as well
			cfg.Project = p.Name
			cfg.ProjectID = int(p.ID)

			if err := cfg.Save(rootDir); err != nil {
				return fmt.Errorf("failed to save init config: %w", err)
			}

			fmt.Println("Created new talis project:", p.Name, p.ID)

			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().IntVarP(&validators, "validators", "v", 4, "number of validators to create")
	cmd.Flags().StringVarP(&chainID, "chainid", "c", "talis", "Chain ID (required)")
	cmd.MarkFlagRequired("chainID")
	cmd.Flags().StringVarP(&talisIP, "talis-ip", "t", "", "ip of the talis server (required)")
	cmd.MarkFlagRequired("talis-ip")
	cmd.Flags().StringVarP(&talisKey, "talis-key", "k", "", "key of the talis server (required)")
	cmd.MarkFlagRequired("talis-key")
	cmd.Flags().StringVarP(&project, "project", "p", "test", "the name of the project (required)")
	cmd.MarkFlagRequired("project")
	cmd.Flags().StringVarP(&userSSH, "user-ssh-name", "s", "", "the name of the SSH key (required)")
	cmd.MarkFlagRequired("user-ssh-name")
	cmd.Flags().IntVarP(&userID, "user-id", "u", 0, "the user's ID")

	return cmd
}

func upCmd() *cobra.Command {
	var rootDir string
	var cfgPath string

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Uses the config to spin up a distributed network",
		Long:  "Initialize the Talis network with the provided configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(filepath.Join(rootDir, cfgPath))
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if err := PingTalisServer(cfg.IP, cfg.Key); err != nil {
				return fmt.Errorf("failed to ping Talis server with valid IP and Key: %w", err)
			}

			return CreateValidators(cfg)
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "name of the config")

	return cmd
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
			if err := os.MkdirAll(target, info.Mode()); err != nil {
				return err
			}
			return nil
		}

		// it's a file; copy it
		return copyFile(path, target, info.Mode())
	})
}

// copyFile copies a single file from src to dest preserving permissions
func copyFile(srcFile, destFile string, perm os.FileMode) error {
	src, err := os.Open(srcFile)
	if err != nil {
		return err
	}
	defer src.Close()

	dest, err := os.OpenFile(destFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer dest.Close()

	_, err = io.Copy(dest, src)
	return err
}

func CreateValidators(cfg Config) error {
	// Create a new validator using the Talis API
	ireqs := make([]types.InstanceRequest, len(cfg.Validators))
	for _, v := range cfg.Validators {
		fmt.Println("Creating validator", v.Name)
		ireq := types.InstanceRequest{
			Name:              v.Name,
			OwnerID:           uint(cfg.UserID),
			ProjectName:       cfg.Project,
			Provider:          models.ProviderDO,
			Region:            v.Region,
			Size:              v.Slug,             // Provider-specific size slug
			Image:             "ubuntu-22-04-x64", // Provider-specific image slug
			SSHKeyName:        cfg.UserSSHKeyName,
			NumberOfInstances: 1, // Creates one instance with this config
			Provision:         false,
			Tags:              []string{"validator", "temp", cfg.Project, cfg.UserSSHKeyName},
			Volumes:           []types.VolumeConfig{},
		}

		ireqs = append(ireqs, ireq)
	}

	// Add more types.InstanceRequest structs here to create multiple instances in one call
	tapi, err := cfg.TalisClient(time.Second * 120)
	if err != nil {
		return err
	}

	err = tapi.CreateInstance(context.Background(), ireqs)
	if err != nil {
		log.Fatalf("Error creating instance(s): %v", err)
	}

	fmt.Println("Issued request to start instance(s) successfully")

	timeout, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	return checkReady(timeout, cfg, tapi)
}

func checkReady(ctx context.Context, cfg Config, tapi talisClient.Client) error {
	expected := make(map[string]bool, len(cfg.Validators))
	for _, v := range cfg.Validators {
		expected[v.Name] = true
	}

	ready := make(map[string]bool)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout hit waiting for all instances to be ready: %v", ctx.Err())
		default:
			status := models.InstanceStatusReady
			lopts := &models.ListOptions{
				Limit:          1000,
				IncludeDeleted: false,
				StatusFilter:   models.StatusFilterEqual,
				InstanceStatus: &status,
			}
			instances, err := tapi.GetInstances(context.Background(), lopts)
			if err != nil {
				return err
			}

			for _, i := range instances {
				if _, has := expected[i.Name]; !has {
					continue
				}

				if i.Status == models.InstanceStatusReady {
					ready[i.Name] = true
				}
			}

			if len(ready) == len(expected) {
				return nil
			}

			time.Sleep(5 * time.Second)

			fmt.Println("Waiting for instances to be ready...", len(ready), "of", len(expected), "ready")
		}
	}
}
