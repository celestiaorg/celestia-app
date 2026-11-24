package docker_e2e

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v12/test/util/genesis"
	"github.com/celestiaorg/tastora/framework/docker/consts"
	tastoracontainertypes "github.com/celestiaorg/tastora/framework/docker/container"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/moby/moby/api/types/container"
	mobyclient "github.com/moby/moby/client"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"celestiaorg/celestia-app/test/docker-e2e/internal"
)

const (
	// chaosSeedEnv replays a run: set it to the seed a failed run logged.
	chaosSeedEnv = "CHAOS_SEED"
	// chaosStepsEnv and chaosMaxFaultEnv shorten or lengthen a run.
	chaosStepsEnv    = "CHAOS_STEPS"
	chaosMaxFaultEnv = "CHAOS_MAX_FAULT"
	// chaosRequireNetemEnv fails the test when the kernel has no netem, instead
	// of skipping the delay and loss steps. The nightly sets it.
	chaosRequireNetemEnv = "CHAOS_REQUIRE_NETEM"
	// chaosLogDirEnv, when set, receives the full log of every node on failure.
	chaosLogDirEnv = "CHAOS_LOG_DIR"

	chaosValidators      = 4
	chaosDefaultSteps    = 8
	chaosMinFault        = 5 * time.Second
	chaosDefaultMaxFault = 30 * time.Second
	// chaosRecoveryTimeout bounds how long the chain may take to make progress
	// after a fault heals.
	chaosRecoveryTimeout = 2 * time.Minute
	chaosProgressBlocks  = 3

	// Pumba's network tools image. A container of it joins a node's network
	// namespace with NET_ADMIN to run ip and tc, so the node image needs
	// neither the tools nor the capability.
	netToolsRepository = "ghcr.io/alexei-led/pumba-alpine-nettools"
	netToolsVersion    = "latest@sha256:02a82ce2d64796dbf08fd1277c223eae5b9c9610337ddcb8417da01038cb43a6"
)

// TestChaos runs 4 validators and a full node under txsim load and injects a
// fault schedule derived from a seed: network partitions, latency and packet
// loss with tc netem, and docker pause. Each fault hits less than 1/3 of the
// voting power, except halt-then-heal steps. After each heal the chain must
// make progress, and at the end all nodes must agree on the app hash.
func (s *CelestiaTestSuite) TestChaos() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	ctx := context.TODO()

	seed, steps := s.chaosSchedule()
	t.Logf("chaos seed %d (replay with %s=%d); schedule:\n%s", seed, chaosSeedEnv, seed, internal.FormatSchedule(steps))

	cfg := dockerchain.DefaultConfig(s.client, s.network)
	cfg.Genesis = cfg.Genesis.WithValidators(genesis.NewDefaultValidator("validator3"))
	celestia, fullNode, _ := s.startRecoveryChain(ctx, cfg)
	validators := celestia.Validators
	s.Require().Len(validators, chaosValidators)
	allNodes := celestia.Nodes()

	// Cleanups run in reverse: heal faults, stop the monitor, then save logs.
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		t.Logf("chaos run failed: seed %d (replay with %s=%d); schedule:\n%s", seed, chaosSeedEnv, seed, internal.FormatSchedule(steps))
		s.saveChaosLogs(allNodes)
	})
	s.startConsistencyMonitor(ctx, allNodes)
	faults := &faultInjector{s: s}
	t.Cleanup(func() {
		if err := faults.healAll(context.WithoutCancel(ctx)); err != nil {
			t.Errorf("failed to heal faults: %v", err)
		}
	})

	s.Require().NoError(pullImageWithRetry(ctx, s.client, tastoracontainertypes.NewImage(netToolsRepository, netToolsVersion, "")))
	netem := faults.netemSupported(ctx, fullNode)
	if !netem {
		s.Require().Empty(os.Getenv(chaosRequireNetemEnv), "the kernel has no netem and %s is set", chaosRequireNetemEnv)
		t.Logf("the kernel has no netem (sch_netem): delay and loss steps are skipped")
	}

	// The full node is never faulted, so txsim keeps its gRPC endpoint.
	s.CreateTxSimOn(ctx, celestia, fullNode)

	for _, step := range steps {
		if !netem && (step.Kind == internal.FaultDelay || step.Kind == internal.FaultLoss) {
			t.Logf("%s: skipped, no netem", step)
			continue
		}
		targets := make(cosmos.ChainNodes, len(step.Targets))
		for i, v := range step.Targets {
			targets[i] = validators[v]
		}
		t.Logf("%s: inject", step)
		s.Require().NoError(faults.inject(ctx, step, targets), "failed to inject %s", step)
		// Read the height after every target is faulted: the chain can still
		// commit blocks while the targets are faulted one at a time.
		before, err := fullNode.Height(ctx)
		s.Require().NoError(err)
		t.Logf("step %d: injected at height %d", step.Index, before)
		time.Sleep(step.Duration)
		during, err := fullNode.Height(ctx)
		s.Require().NoError(err)
		s.Require().NoError(faults.healAll(ctx), "failed to heal %s", step)
		t.Logf("step %d: healed at height %d", step.Index, during)
		if step.Halt {
			// One block can still commit if its votes were sent before the fault.
			s.Require().LessOrEqual(during-before, int64(1), "chain advanced during halt step %d", step.Index)
		}
		s.requireChaosProgress(ctx, step, fullNode, targets, during)
	}

	end, err := fullNode.Height(ctx)
	s.Require().NoError(err)
	s.waitForHeight(ctx, allNodes, end)
	s.verifyAppHash(ctx, validators, fullNode, end)
	s.Require().NoError(s.CheckLiveness(ctx, celestia), "validator liveness check failed")
	assertTransactionsIncluded(ctx, t, celestia)
}

// chaosSchedule reads the seed from CHAOS_SEED, or picks and returns a random
// one, and returns the fault schedule for it.
func (s *CelestiaTestSuite) chaosSchedule() (int64, []internal.FaultStep) {
	seed := rand.Int64()
	if v := os.Getenv(chaosSeedEnv); v != "" {
		var err error
		seed, err = strconv.ParseInt(v, 10, 64)
		s.Require().NoError(err, "invalid %s", chaosSeedEnv)
	}
	cfg := internal.ScheduleConfig{
		Validators:  chaosValidators,
		Steps:       chaosDefaultSteps,
		MinDuration: chaosMinFault,
		MaxDuration: chaosDefaultMaxFault,
		HaltSteps:   1,
	}
	if v := os.Getenv(chaosStepsEnv); v != "" {
		var err error
		cfg.Steps, err = strconv.Atoi(v)
		s.Require().NoError(err, "invalid %s", chaosStepsEnv)
	}
	if v := os.Getenv(chaosMaxFaultEnv); v != "" {
		var err error
		cfg.MaxDuration, err = time.ParseDuration(v)
		s.Require().NoError(err, "invalid %s", chaosMaxFaultEnv)
		cfg.MinDuration = min(cfg.MinDuration, cfg.MaxDuration)
	}
	steps, err := internal.GenerateSchedule(seed, cfg)
	s.Require().NoError(err)
	return seed, steps
}

// requireChaosProgress checks that the chain passes height by
// chaosProgressBlocks and that every healed node follows it there.
func (s *CelestiaTestSuite) requireChaosProgress(ctx context.Context, step internal.FaultStep, fullNode *cosmos.ChainNode, targets cosmos.ChainNodes, height int64) {
	err := internal.WaitFor(ctx, chaosRecoveryTimeout, time.Second, func(ctx context.Context) (bool, error) {
		h, err := rpcNode{fullNode}.Height(ctx)
		if err != nil || h < height+chaosProgressBlocks {
			return false, nil
		}
		for _, n := range targets {
			// A healed node can take a moment to answer again.
			if h, err := (rpcNode{n}).Height(ctx); err != nil || h < height+chaosProgressBlocks {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		s.logRollbackDiagnostics(ctx, append(cosmos.ChainNodes{fullNode}, targets...))
	}
	s.Require().NoError(err, "no progress within %s after healing step %d", chaosRecoveryTimeout, step.Index)
}

// saveChaosLogs writes the full log of every node to CHAOS_LOG_DIR when it is
// set, and otherwise logs the tail of each, like the other tests.
func (s *CelestiaTestSuite) saveChaosLogs(nodes cosmos.ChainNodes) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	dir := os.Getenv(chaosLogDirEnv)
	if dir == "" {
		s.logRollbackDiagnostics(ctx, nodes)
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.T().Logf("failed to create %s: %v", dir, err)
		return
	}
	for _, n := range nodes {
		path := filepath.Join(dir, n.Name()+".log")
		if err := os.WriteFile(path, []byte(s.containerLogs(ctx, n.Name())), 0o644); err != nil {
			s.T().Logf("failed to write %s: %v", path, err)
			continue
		}
		s.T().Logf("saved %s logs to %s", n.Name(), path)
	}
}

// faultInjector applies faults to node containers and remembers how to heal them.
type faultInjector struct {
	s     *CelestiaTestSuite
	heals []func(context.Context) error
}

func (f *faultInjector) inject(ctx context.Context, step internal.FaultStep, nodes cosmos.ChainNodes) error {
	for _, n := range nodes {
		var err error
		switch step.Kind {
		case internal.FaultPartition:
			err = f.partition(ctx, n)
		case internal.FaultDelay:
			err = f.netem(ctx, n, fmt.Sprintf("delay %dms %dms", step.Delay.Milliseconds(), step.Jitter.Milliseconds()))
		case internal.FaultLoss:
			err = f.netem(ctx, n, fmt.Sprintf("loss %d%%", step.LossPercent))
		case internal.FaultPause:
			err = f.pause(ctx, n)
		default:
			err = fmt.Errorf("unknown fault kind %q", step.Kind)
		}
		if err != nil {
			return fmt.Errorf("%s on %s: %w", step.Kind, n.Name(), err)
		}
	}
	return nil
}

// healAll heals every injected fault, newest first, and returns the first error.
func (f *faultInjector) healAll(ctx context.Context) error {
	var first error
	for len(f.heals) > 0 {
		heal := f.heals[len(f.heals)-1]
		f.heals = f.heals[:len(f.heals)-1]
		if err := heal(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// partition takes the node's interface down. Unlike docker network
// disconnect, this keeps the node's IP and its published host ports.
func (f *faultInjector) partition(ctx context.Context, n *cosmos.ChainNode) error {
	out, err := f.netTools(ctx, n, "ip route show default | awk '{print $3}'; ip link set eth0 down")
	if err != nil {
		return err
	}
	gateway, err := netip.ParseAddr(strings.TrimSpace(out))
	if err != nil {
		return fmt.Errorf("unexpected default gateway %q: %w", out, err)
	}
	f.heals = append(f.heals, func(ctx context.Context) error {
		// Taking the link down drops the default route, so restore it.
		_, err := f.netTools(ctx, n, "ip link set eth0 up && ip route replace default via "+gateway.String()+" dev eth0")
		return err
	})
	return nil
}

func (f *faultInjector) netem(ctx context.Context, n *cosmos.ChainNode, args string) error {
	if _, err := f.netTools(ctx, n, "tc qdisc replace dev eth0 root netem "+args); err != nil {
		return err
	}
	f.heals = append(f.heals, func(ctx context.Context) error {
		_, err := f.netTools(ctx, n, "tc qdisc del dev eth0 root")
		return err
	})
	return nil
}

func (f *faultInjector) pause(ctx context.Context, n *cosmos.ChainNode) error {
	if _, err := f.s.client.ContainerPause(ctx, n.Name(), mobyclient.ContainerPauseOptions{}); err != nil {
		return err
	}
	f.heals = append(f.heals, func(ctx context.Context) error {
		_, err := f.s.client.ContainerUnpause(ctx, n.Name(), mobyclient.ContainerUnpauseOptions{})
		return err
	})
	return nil
}

// netemSupported reports whether the kernel can run netem, by adding and
// removing it on the loopback interface of node.
func (f *faultInjector) netemSupported(ctx context.Context, n *cosmos.ChainNode) bool {
	out, err := f.netTools(ctx, n, "tc qdisc add dev lo root netem delay 1ms && tc qdisc del dev lo root")
	if err != nil {
		f.s.T().Logf("netem probe failed: %v %s", err, out)
	}
	return err == nil
}

// netTools runs script in a network tools container that shares the network
// namespace of node, and returns its output.
func (f *faultInjector) netTools(ctx context.Context, n *cosmos.ChainNode, script string) (string, error) {
	cli := f.s.client
	created, err := cli.ContainerCreate(ctx, mobyclient.ContainerCreateOptions{
		Config: &container.Config{
			Image:      tastoracontainertypes.NewImage(netToolsRepository, netToolsVersion, "").Ref(),
			Entrypoint: []string{"sh", "-c"},
			Cmd:        []string{script},
			Labels:     map[string]string{consts.CleanupLabel: cli.CleanupLabel()},
		},
		HostConfig: &container.HostConfig{
			NetworkMode: container.NetworkMode("container:" + n.Name()),
			CapAdd:      []string{"NET_ADMIN"},
		},
	})
	if err != nil {
		return "", err
	}
	defer func() {
		_, _ = cli.ContainerRemove(context.WithoutCancel(ctx), created.ID, mobyclient.ContainerRemoveOptions{Force: true})
	}()
	if _, err := cli.ContainerStart(ctx, created.ID, mobyclient.ContainerStartOptions{}); err != nil {
		return "", err
	}
	wait := cli.ContainerWait(ctx, created.ID, mobyclient.ContainerWaitOptions{})
	select {
	case err := <-wait.Error:
		return "", err
	case res := <-wait.Result:
		out := f.s.containerLogs(ctx, created.ID)
		if res.StatusCode != 0 {
			return out, fmt.Errorf("%q exited with %d: %s", script, res.StatusCode, out)
		}
		return out, nil
	}
}
