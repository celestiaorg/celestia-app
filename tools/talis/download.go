package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

func downloadCmd() *cobra.Command {
	var (
		rootDir    string
		cfgPath    string
		SSHKeyPath string
		nodes      string
		table      string
	)

	cmd := &cobra.Command{
		Use:   "download -n <validator-*> -t <table>",
		Short: "Download a file from the Talis network",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators (nodes) found in config")
			}

			nodes, err := filterMatchingInstances(cfg.Validators, nodes)
			if err != nil {
				return fmt.Errorf("failed to filter nodes: %w", err)
			}

			if len(nodes) == 0 {
				return fmt.Errorf("no matching nodes found")
			}

			remotePath := "/root/.celestia-app/data/traces"

			switch table {
			case "logs":
				remotePath = "/root/logs"
			case "*":
			case "":
			default:
				remotePath = filepath.Join(remotePath, table+".jsonl")
			}

			var wg sync.WaitGroup
			for _, node := range nodes {
				wg.Add(1)
				go func() {
					defer wg.Done()
					err := sftpDownload(remotePath, filepath.Join(rootDir, "data"), "root", node.PublicIP, SSHKeyPath)
					if err != nil {
						fmt.Printf("failed to download from %s: %v\n", node.PublicIP, err)
					}
				}()
			}

			wg.Wait()

			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory containing your config")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "path to your network config file")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-key-path", "k", "", "override path to your SSH private key")
	cmd.Flags().StringVarP(&nodes, "nodes", "n", "*", "specify the node(s) to download from. * or specific nodes.")
	cmd.Flags().StringVarP(&table, "tables", "t", "*", "specify a single table to download. 'logs' will download logs")

	cmd.AddCommand(downloadS3DataCmd())

	return cmd
}

func sftpDownload(remotePath, localPath, user, host, sshKeyPath string) error {
	target := fmt.Sprintf("%s@%s:%s", user, host, remotePath)

	// Use `-r` always — safe for both files and dirs in practice
	cmd := exec.Command("sftp",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-i", sshKeyPath,
		"-r", target,
		localPath,
	)

	fmt.Printf("Running: sftp -i %s -r %s %s\n", sshKeyPath, target, localPath)
	return cmd.Run()
}

func filterMatchingInstances(insts []Instance, pattern string) ([]Instance, error) {
	var filtered []Instance
	for _, inst := range insts {
		match, err := matchPattern(pattern, inst.Name)
		if err != nil {
			return nil, err
		}
		if match {
			filtered = append(filtered, inst)
		}
	}
	return filtered, nil
}

// matchPattern compiles a wildcard pattern (e.g., "validator-*")
// to a regex and returns whether it matches the input string.
func matchPattern(pattern, input string) (bool, error) {
	// Escape regex special characters
	escaped := regexp.QuoteMeta(pattern)

	// Convert wildcard '*' to '.*'
	regexPattern := "^" + strings.ReplaceAll(escaped, "\\*", ".*") + "$"

	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return false, err
	}

	return re.MatchString(input), nil
}
