package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/digitalocean/godo"
	"github.com/spf13/cobra"
)

func upCmd() *cobra.Command {
	var rootDir string
	var cfgPath string
	var SSHPubKeyPath string
	var SSHKeyName string
	var DOAPIToken string
	var workers int

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Uses the config to spin up a distributed network",
		Long:  "Initialize the Talis network with the provided configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			// overwrite the config values if flags or env vars are set
			// flag > env > config
			cfg.SSHKeyName = resolveValue(SSHKeyName, EnvVarSSHKeyName, cfg.SSHKeyName)
			cfg.SSHPubKeyPath = resolveValue(SSHPubKeyPath, EnvVarSSHKeyPath, cfg.SSHPubKeyPath)
			cfg.DigitalOceanToken = resolveValue(DOAPIToken, EnvVarDigitalOceanToken, cfg.DigitalOceanToken)

			client, err := NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			if err := client.Up(cmd.Context(), workers); err != nil {
				return fmt.Errorf("failed to spin up network: %w", err)
			}

			if err := client.cfg.Save(rootDir); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&SSHPubKeyPath, "ssh-pub-key-path", "s", "", "path to the user's SSH public key")
	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "name of the config")
	cmd.Flags().StringVarP(&SSHKeyName, "ssh-key-name", "n", "", "name for the SSH key")
	cmd.Flags().StringVarP(&DOAPIToken, "do-api-token", "t", "", "digital ocean api token (defaults to config or env)")
	cmd.Flags().IntVarP(&workers, "workers", "w", 10, "number of concurrent workers for parallel operations (should be > 0)")

	return cmd
}

func deployCmd() *cobra.Command {
	var (
		rootDir      string
		cfgPath      string
		SSHKeyPath   string
		directUpload bool
		workers      int
	)

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Uses the config to spin up a distributed network",
		Long:  "Initialize the Talis network with the provided configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			tarPath := filepath.Join(rootDir, "payload.tar.gz")
			log.Printf("Compressing payload to %s\n", tarPath)
			tarCmd := exec.Command("tar", "-czf", tarPath, "-C", rootDir, "payload")
			if output, err := tarCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("failed to compress payload: %w, output: %s", err, string(output))
			}
			log.Printf("✅ Payload compressed to %s\n", tarPath)

			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			log.Printf("Sending payload to validators...")
			if directUpload {
				return deployPayloadDirect(cfg.Validators, tarPath, SSHKeyPath, "/root", "payload/validator_init.sh", 7*time.Minute, workers)
			}
			return deployPayloadViaS3(cmd.Context(), rootDir, cfg.Validators, tarPath, SSHKeyPath, "/root", "payload/validator_init.sh", 7*time.Minute, cfg.S3Config, workers)
		},
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get user home directory: %v", err)
	}
	defaultKeyPath := filepath.Join(homeDir, ".ssh", "id_ed25519")
	cmd.Flags().StringVarP(&SSHKeyPath, "ssh-pub-key-path", "s", defaultKeyPath, "path to the user's SSH key")
	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "name of the config")
	cmd.Flags().BoolVar(&directUpload, "direct-payload-upload", false, "Upload payload directly to nodes instead of using S3")
	cmd.Flags().IntVarP(&workers, "workers", "w", 10, "number of concurrent workers for parallel operations (should be > 0)")

	return cmd
}

// deployPayloadDirect copies a local archive to each remote host, unpacks it,
// and launches the specified remote script inside a detached tmux session.
// It runs all operations in parallel and returns an error if any host fails.
func deployPayloadDirect(
	ips []Instance,
	archivePath string, // e.g. "./payload.tar.gz"
	sshKeyPath string, // e.g. "~/.ssh/id_ed25519"
	remoteDir string, // e.g. "/root"
	remoteScript string, // e.g. "start.sh"
	timeout time.Duration, // per‐host timeout
	workers int, // number of concurrent workers
) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(ips))
	archiveFile := path.Base(archivePath)

	counter := atomic.Uint32{}

	workerChan := make(chan struct{}, workers)
	for _, inst := range ips {
		workerChan <- struct{}{}
		wg.Add(1)
		go func(inst Instance) {
			defer func() {
				<-workerChan
				wg.Done()
			}()
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			scp := exec.CommandContext(ctx,
				"scp",
				"-i", sshKeyPath,
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				archivePath,
				fmt.Sprintf("root@%s:%s/", inst.PublicIP, remoteDir),
			)
			if out, err := scp.CombinedOutput(); err != nil {
				errCh <- fmt.Errorf("[%s:%s] scp error in region %s: %v\n%s", inst.Name, inst.PublicIP, inst.Region, err, out)
				return
			}

			log.Printf("sent payload to instance 📦 %s: %s\n", inst.Name, inst.PublicIP)

			remoteCmd := strings.Join([]string{
				// unpack
				fmt.Sprintf("tar -xzf %s -C %s", filepath.Join(remoteDir, archiveFile), remoteDir),
				// make sure script is executable
				fmt.Sprintf("chmod +x %s", filepath.Join(remoteDir, remoteScript)),
				// start in a named, detached tmux session
				fmt.Sprintf("tmux new-session -d -s app '%s'", filepath.Join(remoteDir, remoteScript)),
			}, " && ")

			ssh := exec.CommandContext(ctx,
				"ssh",
				"-i", sshKeyPath,
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				fmt.Sprintf("root@%s", inst.PublicIP),
				remoteCmd,
			)
			if out, err := ssh.CombinedOutput(); err != nil {
				errCh <- fmt.Errorf("[%s:%s] ssh error in region %s: %v\n%s", inst.Name, inst.PublicIP, inst.Region, err, out)
				return
			}
			log.Printf("started instance ✅ %s: %s (total %d/%d)\n", inst.Name, inst.PublicIP, counter.Add(1), len(ips))
		}(inst)
	}

	wg.Wait()
	close(errCh)

	var errs []error //nolint:prealloc
	for e := range errCh {
		errs = append(errs, e)
	}
	if len(errs) > 0 {
		sb := "deployment errors:\n"
		for _, e := range errs {
			sb += "- " + e.Error() + "\n"
		}
		return errors.New(sb)
	}
	return nil
}

// deployPayloadViaS3 uploads the payload to S3 first, then has each node download it
func deployPayloadViaS3(
	ctx context.Context,
	rootDir string,
	ips []Instance,
	archivePath string,
	sshKeyPath string,
	remoteDir string,
	remoteScript string,
	timeout time.Duration,
	s3cfg S3Config,
	workers int,
) error {
	cfg, err := LoadConfig(rootDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	s3Client, err := createS3Client(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %w", err)
	}

	log.Printf("Uploading payload to S3...\n")
	s3URL, err := uploadToS3(ctx, s3Client, s3cfg, archivePath)
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	log.Printf("✅ Payload uploaded to S3: %s\n", s3URL)

	var wg sync.WaitGroup
	errCh := make(chan error, len(ips))
	counter := atomic.Uint32{}
	workersChan := make(chan struct{}, workers)

	for _, inst := range ips {
		wg.Add(1)
		go func(inst Instance) {
			workersChan <- struct{}{}
			defer func() {
				wg.Done()
				<-workersChan
			}()
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			archiveFile := filepath.Base(archivePath)
			remoteCmd := strings.Join([]string{
				fmt.Sprintf("curl -L '%s' -o %s", s3URL, filepath.Join(remoteDir, archiveFile)),
				fmt.Sprintf("tar -xzf %s -C %s", filepath.Join(remoteDir, archiveFile), remoteDir),
				fmt.Sprintf("chmod +x %s", filepath.Join(remoteDir, remoteScript)),
				fmt.Sprintf("tmux new-session -d -s app '%s'", filepath.Join(remoteDir, remoteScript)),
			}, " && ")

			ssh := exec.CommandContext(ctx,
				"ssh",
				"-i", sshKeyPath,
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				fmt.Sprintf("root@%s", inst.PublicIP),
				remoteCmd,
			)
			if out, err := ssh.CombinedOutput(); err != nil {
				errCh <- fmt.Errorf("[%s:%s] ssh error in region %s: %v\n%s", inst.Name, inst.PublicIP, inst.Region, err, out)
				return
			}
			log.Printf("started instance ✅ %s: %s (total %d/%d)\n", inst.Name, inst.PublicIP, counter.Add(1), len(ips))
		}(inst)
	}

	wg.Wait()
	close(errCh)

	errs := make([]error, 0)
	for e := range errCh {
		errs = append(errs, e)
	}
	if len(errs) > 0 {
		sb := "deployment errors:\n"
		for _, e := range errs {
			sb += "- " + e.Error() + "\n"
		}
		return errors.New(sb)
	}
	return nil
}

func uploadToS3(ctx context.Context, client *s3.Client, cfg S3Config, localPath string) (string, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	filename := filepath.Base(localPath)
	uploader := manager.NewUploader(client)

	result, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: &cfg.BucketName,
		Key:    &filename,
		ACL:    "public-read",
		Body:   file,
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}

	return result.Location, nil
}

func downCmd() *cobra.Command {
	var rootDir string
	var cfgPath string
	var SSHPubKeyPath string
	var SSHKeyName string
	var DOAPIToken string
	var workers int

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Uses the config to spin down a distributed network",
		Long:  "Destroys the Talis network with the provided configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			// overwrite the config values if flags or env vars are set
			// flag > env > config
			cfg.SSHKeyName = resolveValue(SSHKeyName, EnvVarSSHKeyName, cfg.SSHKeyName)
			cfg.SSHPubKeyPath = resolveValue(SSHPubKeyPath, EnvVarSSHKeyPath, cfg.SSHPubKeyPath)
			cfg.DigitalOceanToken = resolveValue(DOAPIToken, EnvVarDigitalOceanToken, cfg.DigitalOceanToken)

			client, err := NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			if err := client.Down(cmd.Context(), workers); err != nil {
				return fmt.Errorf("failed to spin up network: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&SSHPubKeyPath, "ssh-pub-key-path", "s", "", "path to the user's SSH public key")
	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "name of the config")
	cmd.Flags().StringVarP(&SSHKeyName, "ssh-key-name", "n", "", "name for the SSH key")
	cmd.Flags().StringVarP(&DOAPIToken, "do-api-token", "t", "", "digital ocean api token (defaults to config or env)")
	cmd.Flags().IntVarP(&workers, "workers", "w", 10, "number of concurrent workers for parallel operations (should be > 0)")

	return cmd
}

// resolveValue selects a value based on priority: flag > env > config
func resolveValue(flagVal, envKey, configVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if env := os.Getenv(envKey); env != "" {
		if configVal != "" {
			log.Printf("Using %s from environment variable instead of config", envKey)
		}
		return env
	}
	return configVal
}

func listCmd() *cobra.Command {
	var rootDir string
	var cfgPath string
	var DOAPIToken string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lists the instances in the network",
		Long:  "Lists the instances in the network. Can be used to see if someone is running experiments at the moment",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// overwrite the config values if flags or env vars are set
			// flag > env > config
			cfg.DigitalOceanToken = resolveValue(DOAPIToken, EnvVarDigitalOceanToken, cfg.DigitalOceanToken)

			client, err := NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			opts := &godo.ListOptions{}
			for {
				droplets, resp, err := client.do.Droplets.List(cmd.Context(), opts)
				if err != nil {
					return fmt.Errorf("failed to list droplets: %w", err)
				}

				cnt := 0
				for _, droplet := range droplets {
					// Check if droplet has TalisChainID tag
					hasTalisTag := false
					for _, tag := range droplet.Tags {
						if tag == "talis" {
							hasTalisTag = true
							break
						}
					}

					if hasTalisTag {
						publicIP := ""
						privateIP := ""
						if len(droplet.Networks.V4) > 0 {
							for _, network := range droplet.Networks.V4 {
								if network.Type == "public" && publicIP == "" {
									publicIP = network.IPAddress
								}
								if network.Type == "private" && privateIP == "" {
									privateIP = network.IPAddress
								}
							}
						}

						if cnt == 0 {
							fmt.Printf("%-30s %-10s %-15s %-15s %s\n", "Name", "Status", "Region", "Public IP", "Created")
							fmt.Printf("%-30s %-10s %-15s %-15s %s\n", "----", "------", "------", "---------", "-------")
						}

						fmt.Printf("%-30s %-10s %-15s %-15s %s\n",
							droplet.Name,
							droplet.Status,
							droplet.Region.Slug,
							publicIP,
							droplet.Created)
						cnt++
					}
				}
				fmt.Println("Total number of talis instances: ", cnt)

				// if we are at the last page, break out the for loop
				if resp.Links == nil || resp.Links.IsLastPage() {
					break
				}
				page, err := resp.Links.CurrentPage()
				if err != nil {
					return fmt.Errorf("failed to paginate droplets list: %w", err)
				}

				// set the page we want for the next request
				opts.Page = page + 1
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "name of the config")
	cmd.Flags().StringVarP(&DOAPIToken, "do-api-token", "t", "", "digital ocean api token (defaults to config or env)")

	return cmd
}

func syncCmd() *cobra.Command {
	var rootDir string
	var cfgPath string
	var DOAPIToken string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Syncs config.json with running DigitalOcean droplets",
		Long:  "Queries all DigitalOcean droplets with the 'talis' tag and updates config.json with their current IP addresses",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// overwrite the config values if flags or env vars are set
			// flag > env > config
			cfg.DigitalOceanToken = resolveValue(DOAPIToken, EnvVarDigitalOceanToken, cfg.DigitalOceanToken)

			client, err := NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			// Get all droplets from DigitalOcean
			var allDroplets []godo.Droplet
			opts := &godo.ListOptions{}
			for {
				droplets, resp, err := client.do.Droplets.List(cmd.Context(), opts)
				if err != nil {
					return fmt.Errorf("failed to list droplets: %w", err)
				}
				allDroplets = append(allDroplets, droplets...)

				// if we are at the last page, break out the for loop
				if resp.Links == nil || resp.Links.IsLastPage() {
					break
				}
				page, err := resp.Links.CurrentPage()
				if err != nil {
					return fmt.Errorf("failed to paginate droplets list: %w", err)
				}

				// set the page we want for the next request
				opts.Page = page + 1
			}

			// Filter droplets that have the "talis" tag
			var talisDroplets []godo.Droplet
			for _, droplet := range allDroplets {
				if slices.Contains(droplet.Tags, "talis") {
					talisDroplets = append(talisDroplets, droplet)
				}
			}

			fmt.Printf("Found %d talis droplets in DigitalOcean\n", len(talisDroplets))

			// Track sync results
			var syncedCount int
			var unmatchedDroplets []godo.Droplet
			var unmatchedConfigInstances []Instance

			// Create a map of droplet names to droplets for quick lookup
			dropletMap := make(map[string]godo.Droplet)
			for _, droplet := range talisDroplets {
				dropletMap[droplet.Name] = droplet
			}

			// Check validators
			for i, instance := range cfg.Validators {
				if droplet, exists := dropletMap[instance.Name]; exists {
					// Get IP addresses from droplet
					publicIP := ""
					privateIP := ""
					if len(droplet.Networks.V4) > 0 {
						for _, network := range droplet.Networks.V4 {
							if network.Type == "public" && publicIP == "" {
								publicIP = network.IPAddress
							}
							if network.Type == "private" && privateIP == "" {
								privateIP = network.IPAddress
							}
						}
					}

					// Update config if IPs have changed
					if instance.PublicIP != publicIP || instance.PrivateIP != privateIP {
						cfg.Validators[i].PublicIP = publicIP
						cfg.Validators[i].PrivateIP = privateIP
						fmt.Printf("Synced %s: %s (was: %s)\n", instance.Name, publicIP, instance.PublicIP)
						syncedCount++
					} else {
						fmt.Printf("%s: already up-to-date (%s)\n", instance.Name, publicIP)
					}

					// Remove from droplet map to track processed droplets
					delete(dropletMap, instance.Name)
				} else {
					unmatchedConfigInstances = append(unmatchedConfigInstances, instance)
				}
			}

			// Remaining droplets in the map are unmatched
			for _, droplet := range dropletMap {
				unmatchedDroplets = append(unmatchedDroplets, droplet)
			}

			// Save updated config if any changes were made
			if syncedCount > 0 {
				if err := cfg.Save(rootDir); err != nil {
					return fmt.Errorf("failed to save config: %w", err)
				}
				fmt.Printf("\nConfig saved with %d updates\n", syncedCount)
			}

			// Report unmatched instances
			if len(unmatchedConfigInstances) > 0 {
				fmt.Printf("\nConfig instances not found in DigitalOcean:\n")
				for _, instance := range unmatchedConfigInstances {
					fmt.Printf("  - %s (type: %s)\n", instance.Name, instance.NodeType)
				}
			}

			// Report unmatched droplets
			if len(unmatchedDroplets) > 0 {
				fmt.Printf("\nDigitalOcean droplets not found in config:\n")
				for _, droplet := range unmatchedDroplets {
					publicIP := ""
					if len(droplet.Networks.V4) > 0 {
						for _, network := range droplet.Networks.V4 {
							if network.Type == "public" && publicIP == "" {
								publicIP = network.IPAddress
								break
							}
						}
					}
					fmt.Printf("  - %s (%s, region: %s)\n", droplet.Name, publicIP, droplet.Region.Slug)
				}
			}

			fmt.Printf("\nSync completed: %d instances updated, %d unmatched config instances, %d unmatched droplets\n",
				syncedCount, len(unmatchedConfigInstances), len(unmatchedDroplets))

			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "name of the config")
	cmd.Flags().StringVarP(&DOAPIToken, "do-api-token", "t", "", "digital ocean api token (defaults to config or env)")

	return cmd
}
