package e2e

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/txsim"
	"github.com/celestiaorg/knuu/pkg/knuu"
	"github.com/stretchr/testify/require"
)

// This will only run tests within the v1 major release cycle
const MajorVersion = 1

func TestMinorVersionCompatibility(t *testing.T) {
	if os.Getenv("E2E") == "" {
		t.Skip("skipping e2e test")
	}

	if os.Getenv("E2E_VERSION") == "" {
		t.Skip("skipping e2e test: E2E_VERSION not set")
	}

	versionStr := os.Getenv("E2E_VERSION")
	versions := ParseVersions(versionStr).FilterMajor(MajorVersion).FilterOutReleaseCandidates()
	numNodes := 4
	r := rand.New(rand.NewSource(seed))
	t.Log("Running minor version compatibility test", "versions", versions)

	testnet, err := New(t.Name(), seed)
	require.NoError(t, err)
	t.Cleanup(testnet.Cleanup)

	// preload all docker images
	preloader, err := knuu.NewPreloader()
	require.NoError(t, err)
	t.Cleanup(func() { _ = preloader.EmptyImages() })
	for _, v := range versions {
		err := preloader.AddImage(DockerImageName(v.String()))
		require.NoError(t, err)
	}

	for i := 0; i < numNodes; i++ {
		// each node begins with a random version within the same major version set
		v := versions.Random(r).String()
		t.Log("Starting node", "node", i, "version", v)
		require.NoError(t, testnet.CreateGenesisNode(v, 10000000, 0))
	}

	kr, err := testnet.CreateAccount("alice", 1e12)
	require.NoError(t, err)

	require.NoError(t, testnet.Setup())
	require.NoError(t, testnet.Start())

	sequences := txsim.NewBlobSequence(txsim.NewRange(200, 4000), txsim.NewRange(1, 3)).Clone(5)
	sequences = append(sequences, txsim.NewSendSequence(4, 1000, 100).Clone(5)...)

	errCh := make(chan error)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	opts := txsim.DefaultOptions().WithSeed(seed)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		errCh <- txsim.Run(ctx, testnet.GRPCEndpoints()[0], kr, encCfg, opts, sequences...)
	}()

	for i := 0; i < len(versions)*2; i++ {
		// FIXME: skip the first node because we need them available to
		// submit txs
		if i%numNodes == 0 {
			continue
		}
		newVersion := versions.Random(r).String()
		t.Log("Upgrading node", "node", i%numNodes, "version", newVersion)
		err := testnet.Node(i % numNodes).Upgrade(newVersion)
		require.NoError(t, err)
		time.Sleep(2 * time.Second)
	}
	cancel()

	err = <-errCh
	require.True(t, errors.Is(err, context.Canceled), err.Error())
}

func MajorUpgradeToV2(t *testing.T) {
	if os.Getenv("E2E") == "" {
		t.Skip("skipping e2e test")
	}

	if os.Getenv("E2E_VERSIONS") == "" {
		t.Skip("skipping e2e test: E2E_VERSION not set")
	}
}
