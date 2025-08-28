package main

import (
	"fmt"
	"os"
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
		workers    int
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

			baseTracesRemotePath := "/root/.celestia-app/data/traces"
			remotePaths := []string{}
			switch table {
			case "logs":
				remotePaths = append(remotePaths, "/root/logs")
			case "*", "":
				path := filepath.Join(baseTracesRemotePath, "*")
				remotePaths = append(remotePaths, path)
			default:
				if strings.Contains(table, ",") {
					tables := strings.SplitSeq(table, ",")
					for table := range tables {
						remotePaths = append(remotePaths, filepath.Join(baseTracesRemotePath, table+".jsonl"))
					}
				} else {
					remotePaths = append(remotePaths, filepath.Join(baseTracesRemotePath, table+".jsonl"))
				}
			}

			workers := make(chan struct{}, workers)
			var wg sync.WaitGroup
			for _, node := range nodes {
				wg.Add(1)
				go func() {
					workers <- struct{}{}
					defer func() {
						wg.Done()
						<-workers
					}()
					localPath := filepath.Join(rootDir, "data/", node.Name)
					if strings.Contains(table, ",") {
						filepath.Join(localPath, "traces")
					}
					if err := os.MkdirAll(localPath, 0o755); err != nil {
						fmt.Printf("failed to create directory %s: %v\n", localPath, err)
						return
					}
					for _, remotePath := range remotePaths {
						err := sftpDownload(remotePath, localPath, "root", node.PublicIP, SSHKeyPath)
						if err != nil {
							fmt.Printf("failed to download from %s: %v\n", node.PublicIP, err)
						}
					}
					if table == "logs" {
						// usually, the logs from tmux also include color codes. So we will clean them up.
						logFile := filepath.Join(localPath, "logs")
						content, err := os.ReadFile(logFile)
						if err != nil {
							fmt.Printf("Error reading file: %v\n", err)
							return
						}
						cleaned := stripANSI(string(content))
						// Write back to the same file
						err = os.WriteFile(logFile, []byte(cleaned), 0o644)
						if err != nil {
							fmt.Printf("Error writing file: %v\n", err)
							return
						}
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
	cmd.Flags().StringVarP(&table, "tables", "t", "*", "specify tables to download (comma-separated) or logs to download logs. default is all tables.")
	cmd.Flags().IntVarP(&workers, "workers", "w", 10, "number of concurrent workers for parallel operations (should be > 0)")

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

// Regex to match ANSI escape codes
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI removes ANSI escape codes from the input string, returning a plain text version without formatting codes.
func stripANSI(input string) string {
	return ansiEscape.ReplaceAllString(input, "")
}
