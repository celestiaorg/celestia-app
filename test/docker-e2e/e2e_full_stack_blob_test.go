package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v5/pkg/user"
	"github.com/celestiaorg/celestia-app/v5/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v5/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v5/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
)

func (s *CelestiaTestSuite) TestE2EFullStackBlob() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()

	// deploy celestia-app chain
	cfg := dockerchain.DefaultConfig(s.client, s.network)
	celestia, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).WithChainID("test").Build(ctx)
	s.Require().NoError(err, "failed to build celestia chain")

	err = celestia.Start(ctx)
	s.Require().NoError(err, "failed to start celestia chain")

	// cleanup resources when the test is done
	t.Cleanup(func() {
		if err := celestia.Stop(ctx); err != nil {
			t.Logf("Error stopping celestia chain: %v", err)
		}
	})

	// verify the chain is running
	height, err := celestia.Height(ctx)
	s.Require().NoError(err, "failed to get celestia chain height")
	s.Require().Greater(height, int64(0), "celestia chain height is zero")

	// deploy DA network with bridge, full, and light nodes
	daNetwork := s.deployDANetwork(ctx, celestia)

	// setup TxClient for blob transactions
	txClient, err := dockerchain.SetupTxClient(ctx, celestia.Nodes()[0], cfg)
	s.Require().NoError(err, "failed to setup TxClient")

	// submit blob transactions using TxClient
	blobData := s.submitBlobTransactions(ctx, txClient)

	// wait for blocks to ensure blob transactions are included
	err = wait.ForBlocks(ctx, 3, celestia)
	s.Require().NoError(err, "failed to wait for blocks after blob submission")

	// verify blob retrieval from light node
	s.verifyBlobRetrieval(ctx, daNetwork, blobData)

	t.Log("Full stack blob test completed successfully")
}

// deployDANetwork deploys a data availability network with bridge, full, and light nodes
func (s *CelestiaTestSuite) deployDANetwork(ctx context.Context, celestia *tastoradockertypes.Chain) tastoratypes.DataAvailabilityNetwork {
	t := s.T()

	// configure DA network with bridge, full, and light nodes
	daConfig := tastoradockertypes.Config{
		Logger:          s.logger,
		DockerClient:    s.client,
		DockerNetworkID: s.network,
		DataAvailabilityNetworkConfig: &tastoradockertypes.DataAvailabilityNetworkConfig{
			BridgeNodeCount: 1,
			FullNodeCount:   1,
			LightNodeCount:  1,
			Image: tastoradockertypes.DockerImage{
				Repository: "ghcr.io/celestiaorg/celestia-node",
				Version:    "v0.23.3-mocha", // use latest stable released tag
			},
		},
	}

	// build DA network using provider
	provider := tastoradockertypes.NewProvider(daConfig, t)
	daNetwork, err := provider.GetDataAvailabilityNetwork(ctx)
	s.Require().NoError(err, "failed to create DA network")

	// get chain connection info
	chainID := celestia.GetChainID()

	// get celestia-app node hostname for core connection
	coreNodeHostname, err := celestia.Nodes()[0].GetInternalHostName(ctx)
	s.Require().NoError(err, "failed to get core node hostname")

	// get genesis hash for DA network connection
	genesisHash, err := s.getGenesisHash(ctx, celestia)
	s.Require().NoError(err, "failed to get genesis hash")

	// build CELESTIA_CUSTOM environment variable (use empty string for P2P address)
	celestiaCustom := tastoratypes.BuildCelestiaCustomEnvVar(chainID, genesisHash, "")

	// use "test" as the network name for DA nodes
	daNetworkName := "test"

	// start bridge nodes first
	bridgeNodes := daNetwork.GetBridgeNodes()
	for _, node := range bridgeNodes {
		err := node.Start(ctx,
			tastoratypes.WithChainID(daNetworkName),
			tastoratypes.WithAdditionalStartArguments("--p2p.network", daNetworkName, "--core.ip", coreNodeHostname, "--rpc.addr", "0.0.0.0"),
			tastoratypes.WithEnvironmentVariables(map[string]string{
				"CELESTIA_CUSTOM": celestiaCustom,
				"P2P_NETWORK":     daNetworkName,
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
		celestiaCustomWithP2P := tastoratypes.BuildCelestiaCustomEnvVar(chainID, genesisHash, bridgeP2PAddr)
		err := node.Start(ctx,
			tastoratypes.WithChainID(daNetworkName),
			tastoratypes.WithAdditionalStartArguments("--p2p.network", daNetworkName, "--core.ip", coreNodeHostname, "--rpc.addr", "0.0.0.0"),
			tastoratypes.WithEnvironmentVariables(map[string]string{
				"CELESTIA_CUSTOM": celestiaCustomWithP2P,
				"P2P_NETWORK":     daNetworkName,
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
		celestiaCustomWithP2P := tastoratypes.BuildCelestiaCustomEnvVar(chainID, genesisHash, fullP2PAddr)
		err := node.Start(ctx,
			tastoratypes.WithChainID(daNetworkName),
			tastoratypes.WithAdditionalStartArguments("--p2p.network", daNetworkName, "--rpc.addr", "0.0.0.0"),
			tastoratypes.WithEnvironmentVariables(map[string]string{
				"CELESTIA_CUSTOM": celestiaCustomWithP2P,
				"P2P_NETWORK":     daNetworkName,
			}),
		)
		s.Require().NoError(err, "failed to start light node")
	}

	// cleanup DA network when test is done
	t.Cleanup(func() {
		for _, node := range append(append(bridgeNodes, fullNodes...), lightNodes...) {
			if err := node.Stop(ctx); err != nil {
				t.Logf("Error stopping DA node: %v", err)
			}
		}
	})

	// wait for DA network to be ready
	err = wait.ForBlocks(ctx, 2, celestia)
	s.Require().NoError(err, "failed to wait for DA network initialization")

	t.Log("DA network deployed successfully")
	return daNetwork
}

// submitBlobTransactions creates and submits blob transactions using TxClient
func (s *CelestiaTestSuite) submitBlobTransactions(ctx context.Context, txClient *user.TxClient) []blobData {
	t := s.T()

	var submittedBlobs []blobData

	// create multiple blob transactions with different namespaces
	for i := 0; i < 3; i++ {
		// create random namespace
		ns := testfactory.RandomBlobNamespace()

		// create blob data
		data := []byte(fmt.Sprintf("test blob data %d - %s", i, time.Now().Format(time.RFC3339)))

		// create blob
		blob, err := types.NewV0Blob(ns, data)
		s.Require().NoError(err, "failed to create blob %d", i)

		// submit blob transaction using TxClient
		opts := blobfactory.DefaultTxOpts()
		response, err := txClient.SubmitPayForBlob(ctx, []*share.Blob{blob}, opts...)
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
		for _, retrievedBlob := range retrievedBlobs {
			if retrievedBlob.Namespace == blob.namespace.String() {
				foundBlob = &retrievedBlob
				break
			}
		}
		s.Require().NotNil(foundBlob, "blob %d not found in retrieved blobs", i)

		// verify blob data matches
		s.Require().Equal(blob.data, foundBlob.Data, "blob %d data mismatch", i)
		s.Require().Equal(blob.namespace.String(), foundBlob.Namespace, "blob %d namespace mismatch", i)

		t.Logf("Blob %d successfully retrieved and verified from light node", i)
	}

	t.Log("All blob retrievals verified successfully")
}

// blobData represents submitted blob information for verification
type blobData struct {
	namespace share.Namespace
	data      []byte
	txHash    string
	height    int64
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
