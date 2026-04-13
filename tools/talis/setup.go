package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

// scenarioRunner holds shared state for all scenarios.
// Each scenario populates the fields it cares about via flags.
type scenarioRunner struct {
	// common
	chainID      string
	experiment   string
	validators   int
	region       string
	provider     string
	directory    string
	workers      int
	sshKeyPath   string
	repoRootFlag string
	repoRoot     string

	// scenario-specific (unused fields are simply ignored)
	squareSize          int
	fibreTxsimInstances int
	fibreTxsimConcur    int
	fibreAccounts       int
	download            bool

	// set by the scenario constructor
	defaultSteps []string
}

var stepDescriptions = map[string]string{
	"init":        "Initializing talis network",
	"add":         "Adding validators",
	"build":       "Building talis binaries",
	"up":          "Provisioning VMs",
	"genesis":     "Generating genesis and configs",
	"deploy":      "Deploying binaries and configs",
	"setup-fibre": "Setting up fibre keys and config",
	"start-fibre": "Starting fibre servers",
	"txsim":       "Starting fibre txsim load generators",
	"down":        "Tearing down VMs",
}

// ── parent command ──────────────────────────────────────────────────────

func startCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a network with a predefined scenario",
		Long:  "Start a network using one of the available scenarios. Each scenario defines its own steps, defaults, and flags.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(fibreLoadCmd())

	return cmd
}

// ── fibre-load scenario ─────────────────────────────────────────────────

var fibreLoadSteps = []string{
	"init", "add", "build", "up", "genesis", "deploy", "setup-fibre", "start-fibre", "txsim",
}

func fibreLoadCmd() *cobra.Command {
	s := &scenarioRunner{
		defaultSteps: fibreLoadSteps,
	}

	cmd := &cobra.Command{
		Use:   "fibre-load [flags] [step ...]",
		Short: "Set up a network for fibre load testing",
		Long: `Provisions validators, deploys fibre servers, and starts fibre-txsim load generators.

Available steps:
  init          Initialize talis network
  add           Add validators
  build         Build talis binaries (make build-talis-bins)
  up            Provision VMs
  genesis       Generate genesis and configs
  deploy        Deploy binaries and configs
  setup-fibre   Setup fibre keys and config
  start-fibre   Start fibre servers
  txsim         Start fibre txsim load generators
  down          Tear down VMs (not included in default run)

If no steps are given, all steps except "down" run in order.`,
		RunE: s.run,
	}

	// common flags
	cmd.Flags().StringVarP(&s.chainID, "chainID", "c", "test", "chain ID (matches talis init -c)")
	cmd.Flags().StringVarP(&s.experiment, "experiment", "e", "test", "experiment name (matches talis init -e)")
	cmd.Flags().IntVarP(&s.validators, "validators", "v", 0, "number of validators to add (required)")
	_ = cmd.MarkFlagRequired("validators")
	cmd.Flags().StringVarP(&s.region, "region", "r", "random", "region for validators (random if not set)")
	cmd.Flags().StringVarP(&s.provider, "provider", "p", "digitalocean", "cloud provider (digitalocean, googlecloud)")
	cmd.Flags().StringVarP(&s.directory, "directory", "d", ".", "root directory for talis state")
	cmd.Flags().IntVar(&s.workers, "workers", 10, "concurrent workers for parallel operations")
	cmd.Flags().StringVar(&s.sshKeyPath, "ssh-key-path", "", "path to SSH private key (overrides TALIS_SSH_KEY_PATH env var)")
	cmd.Flags().StringVar(&s.repoRootFlag, "repo-root", "", "path to celestia-app repo root (auto-detected if not set)")

	// fibre-load specific flags
	cmd.Flags().IntVar(&s.squareSize, "square-size", 512, "ODS square size")
	cmd.Flags().IntVar(&s.fibreTxsimInstances, "fibre-txsim-instances", 2, "number of validators to run fibre-txsim on")
	cmd.Flags().IntVar(&s.fibreTxsimConcur, "fibre-txsim-concurrency", 4, "fibre-txsim concurrency per instance")
	cmd.Flags().IntVar(&s.fibreAccounts, "fibre-accounts", 100, "pre-funded fibre accounts per validator")
	cmd.Flags().BoolVar(&s.download, "download", false, "enable download verification in fibre-txsim")

	return cmd
}

// ── shared execution logic ──────────────────────────────────────────────

func (s *scenarioRunner) run(cmd *cobra.Command, args []string) error {
	// Load .env from current directory if present
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to load .env: %w", err)
	}

	if s.repoRootFlag != "" {
		s.repoRoot = s.repoRootFlag
	} else {
		repoRoot, err := detectRepoRoot()
		if err != nil {
			return fmt.Errorf("failed to detect repo root: %w", err)
		}
		s.repoRoot = repoRoot
	}

	explicitSteps := len(args) > 0
	steps := s.defaultSteps
	if explicitSteps {
		steps = args
	}

	// Resume support: skip already-completed steps when using default steps.
	// Explicit step args always run (user knows what they want).
	if !explicitSteps {
		cfg, err := LoadConfig(s.directory)
		if err == nil {
			var filtered []string
			for _, step := range steps {
				if cfg.IsStepCompleted(step) {
					fmt.Printf("Skipping already completed step: %s\n", step)
					continue
				}
				filtered = append(filtered, step)
			}
			steps = filtered
		}
		// If config doesn't exist yet, run all steps (first run).
	}

	if len(steps) == 0 {
		fmt.Println("All steps already completed. Nothing to do.")
		return nil
	}

	stepFuncs := s.stepMap()
	for i, step := range steps {
		fn, ok := stepFuncs[step]
		if !ok {
			return fmt.Errorf("unknown step %q; valid steps: %s, down", step, strings.Join(s.defaultSteps, ", "))
		}
		desc := stepDescriptions[step]
		fmt.Printf("\n==> [%d/%d] %s\n", i+1, len(steps), desc)
		if err := fn(cmd.Context()); err != nil {
			return fmt.Errorf("step %q failed: %w", step, err)
		}
		fmt.Printf("==> [%d/%d] %s done\n", i+1, len(steps), step)

		// Track completed step in config (config exists after init step).
		if cfg, err := LoadConfig(s.directory); err == nil {
			cfg.MarkStepCompleted(step)
			if saveErr := cfg.Save(s.directory); saveErr != nil {
				fmt.Printf("Warning: failed to save step progress: %v\n", saveErr)
			}
		}
	}

	return nil
}

func (s *scenarioRunner) stepMap() map[string]func(context.Context) error {
	return map[string]func(context.Context) error{
		"init":        s.stepInit,
		"add":         s.stepAdd,
		"build":       s.stepBuild,
		"up":          s.stepUp,
		"genesis":     s.stepGenesis,
		"deploy":      s.stepDeploy,
		"setup-fibre": s.stepSetupFibre,
		"start-fibre": s.stepStartFibre,
		"txsim":       s.stepTxsim,
		"down":        s.stepDown,
	}
}

// execStep creates and runs an existing cobra command with the given args.
func (s *scenarioRunner) execStep(ctx context.Context, cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.ExecuteContext(ctx)
}

// ── step implementations ────────────────────────────────────────────────

func (s *scenarioRunner) stepInit(ctx context.Context) error {
	return s.execStep(ctx, initCmd(), []string{
		"-d", s.directory,
		"-c", s.chainID,
		"-e", s.experiment,
		"--with-observability",
		"--src-root", s.repoRoot,
		"-p", s.provider,
	})
}

func (s *scenarioRunner) stepAdd(ctx context.Context) error {
	return s.execStep(ctx, addCmd(), []string{
		"-d", s.directory,
		"-t", "validator",
		"-c", fmt.Sprintf("%d", s.validators),
		"-r", s.region,
		"-p", s.provider,
	})
}

func (s *scenarioRunner) stepBuild(_ context.Context) error {
	buildDir := s.buildDir()
	bins := []string{"celestia-appd", "txsim", "latency-monitor", "fibre", "fibre-txsim", "talis"}
	allPresent := true
	for _, b := range bins {
		if _, err := os.Stat(filepath.Join(buildDir, b)); err != nil {
			allPresent = false
			break
		}
	}
	if allPresent {
		fmt.Println("All binaries already built, skipping (delete build/ to force rebuild)")
		return nil
	}

	fmt.Println("Building talis binaries...")
	cmd := exec.Command("make", "-C", s.repoRoot, "build-talis-bins-rust")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// buildDir returns the path where make build-talis-bins outputs binaries.
// The Makefile hardcodes "talis-setup/build" relative to the repo root.
func (s *scenarioRunner) buildDir() string {
	return filepath.Join(s.repoRoot, "build")
}

func (s *scenarioRunner) stepUp(ctx context.Context) error {
	args := []string{
		"-d", s.directory,
		"-w", fmt.Sprintf("%d", s.workers),
	}
	if s.sshKeyPath != "" {
		args = append(args, "-s", s.sshKeyPath)
	}
	return s.execStep(ctx, upCmd(), args)
}

func (s *scenarioRunner) stepGenesis(ctx context.Context) error {
	return s.execStep(ctx, generateCmd(), []string{
		"-d", s.directory,
		"-s", fmt.Sprintf("%d", s.squareSize),
		"-b", s.buildDir(),
		"--observability-dir", filepath.Join(s.repoRoot, "observability"),
		"--fibre-accounts", fmt.Sprintf("%d", s.fibreAccounts),
	})
}

func (s *scenarioRunner) stepDeploy(ctx context.Context) error {
	args := []string{
		"-d", s.directory,
		"-w", fmt.Sprintf("%d", s.workers),
	}
	if s.sshKeyPath != "" {
		args = append(args, "-s", s.sshKeyPath)
	}
	return s.execStep(ctx, deployCmd(), args)
}

func (s *scenarioRunner) stepSetupFibre(ctx context.Context) error {
	args := []string{
		"-d", s.directory,
		"--fibre-accounts", fmt.Sprintf("%d", s.fibreAccounts),
		"-w", fmt.Sprintf("%d", s.workers),
	}
	if s.sshKeyPath != "" {
		args = append(args, "-k", s.sshKeyPath)
	}
	return s.execStep(ctx, setupFibreCmd(), args)
}

func (s *scenarioRunner) stepStartFibre(ctx context.Context) error {
	args := []string{
		"-d", s.directory,
	}
	if s.sshKeyPath != "" {
		args = append(args, "-k", s.sshKeyPath)
	}
	return s.execStep(ctx, startFibreCmd(), args)
}

func (s *scenarioRunner) stepTxsim(ctx context.Context) error {
	args := []string{
		"-d", s.directory,
		"--instances", fmt.Sprintf("%d", s.fibreTxsimInstances),
		"--concurrency", fmt.Sprintf("%d", s.fibreTxsimConcur),
	}
	if s.sshKeyPath != "" {
		args = append(args, "-k", s.sshKeyPath)
	}
	if s.download {
		args = append(args, "--download")
	}
	return s.execStep(ctx, fibreTxsimCmd(), args)
}

func (s *scenarioRunner) stepDown(ctx context.Context) error {
	args := []string{
		"-d", s.directory,
		"-w", fmt.Sprintf("%d", s.workers),
	}
	if s.sshKeyPath != "" {
		args = append(args, "-s", s.sshKeyPath)
	}
	return s.execStep(ctx, downCmd(), args)
}

// ── helpers ─────────────────────────────────────────────────────────────

// detectRepoRoot finds the repository root by trying go list (for development)
// then falling back to walking up from cwd looking for a Makefile.
func detectRepoRoot() (string, error) {
	// Try go list -m (works when running via go run from within the module)
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err == nil {
		modDir := strings.TrimSpace(string(out))
		// Module is at tools/talis, repo root is two levels up
		repoRoot := filepath.Dir(filepath.Dir(modDir))
		if _, err := os.Stat(filepath.Join(repoRoot, "Makefile")); err == nil {
			return repoRoot, nil
		}
	}

	// Fallback: walk up from cwd looking for Makefile
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("could not find repo root (no Makefile found in parent directories)")
}
