package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	tastoracontainertypes "github.com/celestiaorg/tastora/framework/docker/container"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v6/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	dockerclient "github.com/moby/moby/client"
)

const (
	// celestiaNodeVersion specifies the celestia-node version to be used for the DA network.
	//
	// NOTE: the intention of this test is that it is just a basic sanity check for the entire stack.
	// while the app version will vary on a per-pr and per-tag basis, the node version can remain relatively static.
	// we can bump it as required.
	celestiaNodeVersion    = "v0.23.3-mocha"
	celestiaNodeRepository = "ghcr.io/celestiaorg/celestia-node"
)

// TestE2EFullStackPFB is an E2E test which tests the basic functionality of the entire stack.
// This test does the following:
// - deploys celestia-app
// - deploys a celestia-node full node
// - deploys a celestia-node bridge node
// - deploys a celestia-node light node
// - submits multiple PFBs
// - verifies blob data is retrievable via light node rpc.
func (s *CelestiaTestSuite) TestE2EFullStackPFB() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()

	cfg := dockerchain.DefaultConfig(s.client, s.network)
	cfg.Genesis = cfg.Genesis.WithAppVersion(4) // TODO: currently this node version does not support v5

	celestia, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).WithChainID(appconsts.TestChainID).Build(ctx)
	s.Require().NoError(err, "failed to build celestia chain")

	t.Cleanup(func() {
		if err := celestia.Stop(ctx); err != nil {
			t.Logf("Error stopping celestia chain: %v", err)
		}
	})

	err = celestia.Start(ctx)
	s.Require().NoError(err, "failed to start celestia chain")

	// Record start height for liveness check
	startHeight, err := celestia.Height(ctx)
	s.Require().NoError(err, "failed to get start height")

	// prepare a simple config with one type of each node.
	daConfig := getDAConfig(s.logger, s.client, s.network)

	// create a da network, wiring up the provided chain using the CELESTIA_CUSTOM environment variable.
	daNetwork := s.DeployDANetwork(ctx, celestia, daConfig)

	txClient, err := dockerchain.SetupTxClient(ctx, celestia.Nodes()[0], cfg)
	s.Require().NoError(err, "failed to setup TxClient")

	blobData := s.submitBlobTransactions(ctx, txClient, 5)

	// wait for blocks to ensure blob transactions are included
	err = wait.ForBlocks(ctx, 3, celestia)
	s.Require().NoError(err, "failed to wait for blocks after blob submission")

	// verify blob retrieval from light node
	s.verifyBlobRetrieval(ctx, daNetwork, blobData)

	s.T().Logf("Checking validator liveness from height %d", startHeight)
	s.Require().NoError(
		s.CheckLiveness(ctx, celestia),
		"validator liveness check failed",
	)

	t.Log("Full stack blob test completed successfully")
}

// DeployDANetwork deploys a data availability network with bridge, full, and light nodes
func (s *CelestiaTestSuite) DeployDANetwork(ctx context.Context, celestia *tastoradockertypes.Chain, daConfig tastoradockertypes.Config) tastoratypes.DataAvailabilityNetwork {
	t := s.T()

	// build DA network using provider
	provider := tastoradockertypes.NewProvider(daConfig, t)
	daNetwork, err := provider.GetDataAvailabilityNetwork(ctx)
	s.Require().NoError(err, "failed to create DA network")

	// get celestia-app node hostname for core connection
	coreNodeHostname, err := celestia.Nodes()[0].GetInternalHostName(ctx)
	s.Require().NoError(err, "failed to get core node hostname")

	// get genesis hash for DA network connection
	genesisHash, err := s.getGenesisHash(ctx, celestia)
	s.Require().NoError(err, "failed to get genesis hash")

	// build CELESTIA_CUSTOM environment variable (empty string for bridge node is fine.)
	celestiaCustom := tastoratypes.BuildCelestiaCustomEnvVar(celestia.GetChainID(), genesisHash, "")

	// start bridge nodes first
	bridgeNodes := daNetwork.GetBridgeNodes()
	for _, node := range bridgeNodes {
		err := node.Start(ctx,
			tastoratypes.WithChainID(celestia.GetChainID()),
			tastoratypes.WithAdditionalStartArguments("--p2p.network", celestia.GetChainID(), "--core.ip", coreNodeHostname, "--rpc.addr", "0.0.0.0"),
			tastoratypes.WithEnvironmentVariables(map[string]string{
				"CELESTIA_CUSTOM": celestiaCustom,
				"P2P_NETWORK":     celestia.GetChainID(),
			}),
		)
		s.Require().NoError(err, "failed to start bridge node")
	}

	// get P2P info from bridge node for full nodes
	var bridgeP2PAddr string
	if len(bridgeNodes) > 0 {
		p2pInfo, err := bridgeNodes[0].GetP2PInfo(ctx)
		s.Require().NoError(err, "failed to get bridge node p2p info")
		bridgeP2PAddr, err = p2pInfo.GetP2PAddress()
		s.Require().NoError(err, "failed to get bridge node p2p address")
	}

	// start full nodes
	fullNodes := daNetwork.GetFullNodes()
	for _, node := range fullNodes {
		err := node.Start(ctx,
			tastoratypes.WithChainID(celestia.GetChainID()),
			tastoratypes.WithAdditionalStartArguments("--p2p.network", celestia.GetChainID(), "--core.ip", coreNodeHostname, "--rpc.addr", "0.0.0.0"),
			tastoratypes.WithEnvironmentVariables(map[string]string{
				"CELESTIA_CUSTOM": tastoratypes.BuildCelestiaCustomEnvVar(celestia.GetChainID(), genesisHash, bridgeP2PAddr),
				"P2P_NETWORK":     celestia.GetChainID(),
			}),
		)
		s.Require().NoError(err, "failed to start full node")
	}

	// get P2P info from full node for light nodes
	var fullP2PAddr string
	if len(fullNodes) > 0 {
		p2pInfo, err := fullNodes[0].GetP2PInfo(ctx)
		s.Require().NoError(err, "failed to get full node p2p info")
		fullP2PAddr, err = p2pInfo.GetP2PAddress()
		s.Require().NoError(err, "failed to get full node p2p address")
	}

	// start light nodes
	lightNodes := daNetwork.GetLightNodes()
	for _, node := range lightNodes {
		err := node.Start(ctx,
			tastoratypes.WithChainID(celestia.GetChainID()),
			tastoratypes.WithAdditionalStartArguments("--p2p.network", celestia.GetChainID(), "--rpc.addr", "0.0.0.0"),
			tastoratypes.WithEnvironmentVariables(map[string]string{
				"CELESTIA_CUSTOM": tastoratypes.BuildCelestiaCustomEnvVar(celestia.GetChainID(), genesisHash, fullP2PAddr),
				"P2P_NETWORK":     celestia.GetChainID(),
			}),
		)
		s.Require().NoError(err, "failed to start light node")
	}

	// cleanup DA network when test is done
	t.Cleanup(func() {
		if err := stopDANetwork(ctx, daNetwork); err != nil {
			t.Logf("Error stopping DA network: %v", err)
		}
	})

	t.Log("DA network deployed successfully")
	return daNetwork
}

// submitBlobTransactions creates and submits blob transactions using TxClient
func (s *CelestiaTestSuite) submitBlobTransactions(ctx context.Context, txClient *user.TxClient, numTransactions int) []blobData {
	t := s.T()

	var submittedBlobs []blobData

	// create multiple blob transactions with different namespaces
	for i := 0; i < numTransactions; i++ {
		// create random namespace
		ns := testfactory.RandomBlobNamespace()

		// create blob data
		data := []byte(fmt.Sprintf("test blob data %d - %s", i, time.Now().Format(time.RFC3339)))

		// create blob
		blob, err := types.NewV0Blob(ns, data)
		s.Require().NoError(err, "failed to create blob %d", i)

		// submit blob transaction using TxClient
		response, err := txClient.SubmitPayForBlob(ctx, []*share.Blob{blob}, user.SetGasLimit(200000), user.SetFee(5000))
		s.Require().NoError(err, "failed to submit blob transaction %d", i)
		s.Require().Equal(uint32(0), response.Code, "blob transaction %d failed with code %d", i, response.Code)

		t.Logf("Blob %d submitted successfully: TxHash=%s, Height=%d", i, response.TxHash, response.Height)

		// store blob data for later verification
		submittedBlobs = append(submittedBlobs, blobData{
			namespace: ns,
			data:      data,
			txHash:    response.TxHash,
			height:    response.Height,
		})
	}

	t.Logf("Successfully submitted %d blob transactions", len(submittedBlobs))
	return submittedBlobs
}

// verifyBlobRetrieval verifies that blob data can be retrieved from the light node
func (s *CelestiaTestSuite) verifyBlobRetrieval(ctx context.Context, daNetwork tastoratypes.DataAvailabilityNetwork, blobData []blobData) {
	t := s.T()

	// get light node from DA network
	lightNodes := daNetwork.GetLightNodes()
	s.Require().NotEmpty(lightNodes, "no light nodes available in DA network")

	lightNode := lightNodes[0]
	s.Require().NotNil(lightNode, "light node is nil")

	// verify each blob can be retrieved
	for i, blob := range blobData {
		t.Logf("Verifying blob %d retrieval from light node", i)

		// attempt to retrieve blob from light node using GetAllBlobs
		retrievedBlobs, err := lightNode.GetAllBlobs(ctx, uint64(blob.height), []share.Namespace{blob.namespace})
		s.Require().NoError(err, "failed to retrieve blob %d from light node", i)
		s.Require().NotEmpty(retrievedBlobs, "no blobs retrieved for blob %d", i)

		// find the matching blob
		var foundBlob *tastoratypes.Blob

		// Convert the original namespace to base64 for comparison (since API returns base64)
		expectedNamespaceB64 := base64.StdEncoding.EncodeToString(blob.namespace.Bytes())
		for _, retrievedBlob := range retrievedBlobs {
			if retrievedBlob.Namespace == expectedNamespaceB64 {
				foundBlob = &retrievedBlob
				break
			}
		}
		s.Require().NotNil(foundBlob, "blob %d not found in retrieved blobs", i)

		// verify blob data matches (decode base64 data from API response)
		retrievedData, err := base64.StdEncoding.DecodeString(foundBlob.Data)
		s.Require().NoError(err, "failed to decode blob %d data from base64", i)
		s.Require().Equal(blob.data, retrievedData, "blob %d data mismatch", i)
		s.Require().Equal(expectedNamespaceB64, foundBlob.Namespace, "blob %d namespace mismatch", i)

		t.Logf("Blob %d successfully retrieved and verified from light node", i)
	}

	t.Log("All blob retrievals verified successfully")
}

// getGenesisHash retrieves the genesis hash from the celestia chain
func (s *CelestiaTestSuite) getGenesisHash(ctx context.Context, celestia *tastoradockertypes.Chain) (string, error) {
	node := celestia.GetNodes()[0]
	c, err := node.GetRPCClient()
	if err != nil {
		return "", fmt.Errorf("failed to get node client: %w", err)
	}

	first := int64(1)
	block, err := c.Block(ctx, &first)
	if err != nil {
		return "", fmt.Errorf("failed to get block: %w", err)
	}

	genesisHash := block.Block.Header.Hash().String()
	if genesisHash == "" {
		return "", fmt.Errorf("genesis hash is empty")
	}

	return genesisHash, nil
}

// getDAConfig returns a DataAvailabilityNetworkConfig for the given networkID with a single bridge, full, and light node.
func getDAConfig(logger *zap.Logger, client *dockerclient.Client, networkID string) tastoradockertypes.Config {
	return tastoradockertypes.Config{
		Logger:          logger,
		DockerClient:    client,
		DockerNetworkID: networkID,
		DataAvailabilityNetworkConfig: &tastoradockertypes.DataAvailabilityNetworkConfig{
			BridgeNodeCount: 1,
			FullNodeCount:   1,
			LightNodeCount:  1,
			Image: tastoracontainertypes.Image{
				Repository: celestiaNodeRepository,
				Version:    celestiaNodeVersion,
			},
		},
	}
}

// stopDANetwork stops all nodes in the Data Availability Network, including bridge, full, and light nodes.
// Returns an error if any node fails to stop.
//
// remove after: https://github.com/celestiaorg/tastora/issues/74 is done
func stopDANetwork(ctx context.Context, daNetwork tastoratypes.DataAvailabilityNetwork) error {
	var errs []error
	for _, node := range daNetwork.GetBridgeNodes() {
		if err := node.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop bridge node: %w", err))
		}
	}
	for _, node := range daNetwork.GetFullNodes() {
		if err := node.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop full node: %w", err))
		}
	}
	for _, node := range daNetwork.GetLightNodes() {
		if err := node.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop light node: %w", err))
		}
	}
	return errors.Join(errs...)
}

// blobData represents submitted blob information for verification
type blobData struct {
	namespace share.Namespace
	data      []byte
	txHash    string
	height    int64
}
