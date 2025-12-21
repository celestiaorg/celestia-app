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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/digitalocean/godo"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

func upCmd() *cobra.Command {
	var rootDir string
	var cfgPath string
	var SSHPubKeyPath string
	var SSHKeyName string
	var DOAPIToken string
	var GCProject string
	var GCKeyJSONPath string
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
			cfg.GoogleCloudProject = resolveValue(GCProject, EnvVarGoogleCloudProject, cfg.GoogleCloudProject)
			cfg.GoogleCloudKeyJSONPath = resolveValue(GCKeyJSONPath, EnvVarGoogleCloudKeyJSONPath, cfg.GoogleCloudKeyJSONPath)

			if err := checkForRunningExperiments(cmd.Context(), cfg); err != nil {
				return err
			}

			client, err := NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			if err := client.Up(cmd.Context(), workers); err != nil {
				return fmt.Errorf("failed to spin up network: %w", err)
			}

			if err := client.GetConfig().Save(rootDir); err != nil {
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
	cmd.Flags().StringVar(&GCProject, "gc-project", "", "google cloud project (defaults to config or env)")
	cmd.Flags().StringVar(&GCKeyJSONPath, "gc-key-json-path", "", "path to google cloud service account key JSON file (defaults to config or env)")
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
				if err := deployPayloadDirect(cfg.Validators, tarPath, SSHKeyPath, "/root", "payload/validator_init.sh", 7*time.Minute, workers); err != nil {
					return err
				}
				return deployMetricsIfConfigured(cmd.Context(), cfg, rootDir, SSHKeyPath, directUpload, workers)
			}
			if err := deployPayloadViaS3(cmd.Context(), rootDir, cfg.Validators, tarPath, SSHKeyPath, "/root", "payload/validator_init.sh", 7*time.Minute, cfg.S3Config, workers); err != nil {
				return err
			}
			return deployMetricsIfConfigured(cmd.Context(), cfg, rootDir, SSHKeyPath, directUpload, workers)
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

func deployMetricsIfConfigured(ctx context.Context, cfg Config, rootDir, sshKeyPath string, directUpload bool, workers int) error {
	if len(cfg.Metrics) == 0 {
		return nil
	}

	metricsTarPath := filepath.Join(rootDir, "metrics-payload.tar.gz")
	log.Printf("Compressing metrics payload to %s\n", metricsTarPath)
	tarCmd := exec.Command("tar", "-czf", metricsTarPath, "-C", filepath.Join(rootDir, "payload"), "metrics")
	if output, err := tarCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to compress metrics payload: %w, output: %s", err, string(output))
	}
	log.Printf("✅ Metrics payload compressed to %s\n", metricsTarPath)

	log.Printf("Sending metrics payload to metrics nodes...")
	var err error
	if directUpload {
		err = deployMetricsPayloadDirect(cfg.Metrics, metricsTarPath, sshKeyPath, "/root", 15*time.Minute, workers)
	} else {
		err = deployMetricsPayloadViaS3(ctx, rootDir, cfg.Metrics, metricsTarPath, sshKeyPath, "/root", 15*time.Minute, cfg.S3Config, workers)
	}
	if err != nil {
		return err
	}

	printGrafanaInfo(cfg.Metrics, rootDir)
	return nil
}

// printGrafanaInfo prints the Grafana URL and credentials after successful metrics deployment.
func printGrafanaInfo(metricsNodes []Instance, rootDir string) {
	password := readGrafanaPassword(rootDir)

	fmt.Println()
	fmt.Println("Grafana available at:")
	for _, node := range metricsNodes {
		fmt.Printf("  http://%s:3000  (credentials: admin/%s)\n", node.PublicIP, password)
	}
	fmt.Println()
}

// readGrafanaPassword reads the Grafana password from the .env file in the payload directory.
func readGrafanaPassword(rootDir string) string {
	envPath := filepath.Join(rootDir, "payload", "metrics", "docker", ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return "admin" // fallback to default
	}
	// Parse GRAFANA_PASSWORD=<password> from .env
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "GRAFANA_PASSWORD=") {
			return strings.TrimPrefix(line, "GRAFANA_PASSWORD=")
		}
	}
	return "admin" // fallback to default
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
		var sb strings.Builder
		sb.WriteString("deployment errors:\n")
		for _, e := range errs {
			sb.WriteString("- " + e.Error() + "\n")
		}
		return errors.New(sb.String())
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
		var sb strings.Builder
		sb.WriteString("deployment errors:\n")
		for _, e := range errs {
			sb.WriteString("- " + e.Error() + "\n")
		}
		return errors.New(sb.String())
	}
	return nil
}

// deployMetricsPayloadDirect copies a metrics archive to each metrics host, unpacks it,
// installs prerequisites, and launches the metrics stack in a detached tmux session.
func deployMetricsPayloadDirect(
	ips []Instance,
	archivePath string,
	sshKeyPath string,
	remoteDir string,
	timeout time.Duration,
	workers int,
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

			log.Printf("sent metrics payload to instance 📦 %s: %s\n", inst.Name, inst.PublicIP)

			remoteCmd := strings.Join([]string{
				fmt.Sprintf("tar -xzf %s -C %s", filepath.Join(remoteDir, archiveFile), remoteDir),
				fmt.Sprintf("chmod +x %s %s",
					filepath.Join(remoteDir, "metrics/install_metrics.sh"),
					filepath.Join(remoteDir, "metrics/start_metrics.sh"),
				),
				filepath.Join(remoteDir, "metrics/install_metrics.sh"),
				"tmux has-session -t metrics && tmux kill-session -t metrics || true",
				fmt.Sprintf("tmux new-session -d -s metrics '%s'", filepath.Join(remoteDir, "metrics/start_metrics.sh")),
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
			log.Printf("started metrics instance ✅ %s: %s (total %d/%d)\n", inst.Name, inst.PublicIP, counter.Add(1), len(ips))
		}(inst)
	}

	wg.Wait()
	close(errCh)

	var errs []error //nolint:prealloc
	for e := range errCh {
		errs = append(errs, e)
	}
	if len(errs) > 0 {
		var sb strings.Builder
		sb.WriteString("metrics deployment errors:\n")
		for _, e := range errs {
			sb.WriteString("- " + e.Error() + "\n")
		}
		return errors.New(sb.String())
	}
	return nil
}

// deployMetricsPayloadViaS3 uploads the metrics payload to S3 first, then has each node download it.
func deployMetricsPayloadViaS3(
	ctx context.Context,
	rootDir string,
	ips []Instance,
	archivePath string,
	sshKeyPath string,
	remoteDir string,
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

	log.Printf("Uploading metrics payload to S3...\n")
	s3URL, err := uploadToS3(ctx, s3Client, s3cfg, archivePath)
	if err != nil {
		return fmt.Errorf("failed to upload metrics payload to S3: %w", err)
	}

	log.Printf("✅ Metrics payload uploaded to S3: %s\n", s3URL)

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
				fmt.Sprintf("chmod +x %s %s",
					filepath.Join(remoteDir, "metrics/install_metrics.sh"),
					filepath.Join(remoteDir, "metrics/start_metrics.sh"),
				),
				filepath.Join(remoteDir, "metrics/install_metrics.sh"),
				"tmux has-session -t metrics && tmux kill-session -t metrics || true",
				fmt.Sprintf("tmux new-session -d -s metrics '%s'", filepath.Join(remoteDir, "metrics/start_metrics.sh")),
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
			log.Printf("started metrics instance ✅ %s: %s (total %d/%d)\n", inst.Name, inst.PublicIP, counter.Add(1), len(ips))
		}(inst)
	}

	wg.Wait()
	close(errCh)

	errs := make([]error, 0)
	for e := range errCh {
		errs = append(errs, e)
	}
	if len(errs) > 0 {
		var sb strings.Builder
		sb.WriteString("metrics deployment errors:\n")
		for _, e := range errs {
			sb.WriteString("- " + e.Error() + "\n")
		}
		return errors.New(sb.String())
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
	var GCProject string
	var GCKeyJSONPath string
	var workers int
	var all bool

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Uses the config to spin down a distributed network",
		Long:  "Destroys the Talis network with the provided configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil && !all {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// overwrite the config values if flags or env vars are set
			// flag > env > config
			cfg.DigitalOceanToken = resolveValue(DOAPIToken, EnvVarDigitalOceanToken, cfg.DigitalOceanToken)
			cfg.GoogleCloudProject = resolveValue(GCProject, EnvVarGoogleCloudProject, cfg.GoogleCloudProject)
			cfg.GoogleCloudKeyJSONPath = resolveValue(GCKeyJSONPath, EnvVarGoogleCloudKeyJSONPath, cfg.GoogleCloudKeyJSONPath)

			if all {
				return destroyAllInstances(cmd.Context(), cfg, workers)
			}

			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			cfg.SSHKeyName = resolveValue(SSHKeyName, EnvVarSSHKeyName, cfg.SSHKeyName)
			cfg.SSHPubKeyPath = resolveValue(SSHPubKeyPath, EnvVarSSHKeyPath, cfg.SSHPubKeyPath)

			client, err := NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			if err := client.Down(cmd.Context(), workers); err != nil {
				return fmt.Errorf("failed to spin down network: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&SSHPubKeyPath, "ssh-pub-key-path", "s", "", "path to the user's SSH public key")
	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "name of the config")
	cmd.Flags().StringVarP(&SSHKeyName, "ssh-key-name", "n", "", "name for the SSH key")
	cmd.Flags().StringVarP(&DOAPIToken, "do-api-token", "t", "", "digital ocean api token (defaults to config or env)")
	cmd.Flags().StringVar(&GCProject, "gc-project", "", "google cloud project (defaults to config or env)")
	cmd.Flags().StringVar(&GCKeyJSONPath, "gc-key-json-path", "", "path to google cloud service account key JSON file (defaults to config or env)")
	cmd.Flags().IntVarP(&workers, "workers", "w", 10, "number of concurrent workers for parallel operations (should be > 0)")
	cmd.Flags().BoolVar(&all, "all", false, "destroy all talis instances across all providers and all experiments")

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
	var GCProject string
	var GCKeyJSONPath string

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
			cfg.GoogleCloudProject = resolveValue(GCProject, EnvVarGoogleCloudProject, cfg.GoogleCloudProject)
			cfg.GoogleCloudKeyJSONPath = resolveValue(GCKeyJSONPath, EnvVarGoogleCloudKeyJSONPath, cfg.GoogleCloudKeyJSONPath)

			client, err := NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			return client.List(cmd.Context())
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "name of the config")
	cmd.Flags().StringVarP(&DOAPIToken, "do-api-token", "t", "", "digital ocean api token (defaults to config or env)")
	cmd.Flags().StringVar(&GCProject, "gc-project", "", "google cloud project (defaults to config or env)")
	cmd.Flags().StringVar(&GCKeyJSONPath, "gc-key-json-path", "", "path to google cloud service account key JSON file (defaults to config or env)")

	return cmd
}

func checkForRunningExperiments(ctx context.Context, cfg Config) error {
	var hasRunningExperiments bool

	if cfg.DigitalOceanToken != "" {
		tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.DigitalOceanToken})
		doClient := godo.NewClient(oauth2.NewClient(ctx, tokenSource))
		running, err := checkForRunningDOExperiments(ctx, doClient, cfg.Experiment, cfg.ChainID)
		if err != nil {
			log.Printf("⚠️  Warning: failed to check DigitalOcean for running experiments: %v", err)
		} else if running {
			hasRunningExperiments = true
			log.Printf("⚠️  Found experiment '%s' with chainID '%s' already running in DigitalOcean", cfg.Experiment, cfg.ChainID)
		}
	}

	if cfg.GoogleCloudProject != "" {
		opts, err := gcClientOptions(cfg)
		if err != nil {
			log.Printf("⚠️  Warning: failed to create Google Cloud client options: %v", err)
		} else {
			running, err := checkForRunningGCExperiments(ctx, cfg.GoogleCloudProject, opts, cfg.Experiment, cfg.ChainID)
			if err != nil {
				log.Printf("⚠️  Warning: failed to check Google Cloud for running experiments: %v", err)
			} else if running {
				hasRunningExperiments = true
				log.Printf("⚠️  Found experiment '%s' with chainID '%s' already running in Google Cloud", cfg.Experiment, cfg.ChainID)
			}
		}
	}

	if hasRunningExperiments {
		return fmt.Errorf("experiment '%s' with chainID '%s' is already running", cfg.Experiment, cfg.ChainID)
	}

	return nil
}

func destroyAllInstances(ctx context.Context, cfg Config, workers int) error {
	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	if cfg.DigitalOceanToken != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Println("Destroying all DigitalOcean instances...")
			tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.DigitalOceanToken})
			doClient := godo.NewClient(oauth2.NewClient(ctx, tokenSource))
			if _, err := destroyAllTalisDroplets(ctx, doClient, workers); err != nil {
				errCh <- fmt.Errorf("DigitalOcean: %w", err)
			}
		}()
	}

	if cfg.GoogleCloudProject != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Println("Destroying all Google Cloud instances...")
			opts, err := gcClientOptions(cfg)
			if err != nil {
				errCh <- fmt.Errorf("google Cloud client options: %w", err)
				return
			}
			if _, err := destroyAllTalisGCInstances(ctx, cfg.GoogleCloudProject, opts, workers); err != nil {
				errCh <- fmt.Errorf("google Cloud: %w", err)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	errs := make([]error, 0, 2)
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		var sb strings.Builder
		sb.WriteString("errors destroying instances:\n")
		for _, err := range errs {
			sb.WriteString("- " + err.Error() + "\n")
		}
		return errors.New(sb.String())
	}

	log.Println("✅ All talis instances destroyed")
	return nil
}
