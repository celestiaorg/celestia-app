package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/celestiaorg/tastora/framework/docker/dataavailability"
	"github.com/celestiaorg/tastora/framework/docker/evstack/evmsingle"
	"github.com/celestiaorg/tastora/framework/docker/evstack/reth"
	"github.com/celestiaorg/tastora/framework/docker/hyperlane"
	"github.com/celestiaorg/tastora/framework/testutil/evm"
	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestHyperlaneTestSuite(t *testing.T) {
	suite.Run(t, new(HyperlaneTestSuite))
}

type HyperlaneTestSuite struct {
	CelestiaTestSuite
}

func (s *HyperlaneTestSuite) SetupSuite() {
	s.logger = zaptest.NewLogger(s.T())
	s.logger.Info("Setting up Celestia test suite: " + s.T().Name())
	s.client, s.network = tastoradockertypes.Setup(s.T())

	tag, err := dockerchain.GetCelestiaTagStrict()
	s.Require().NoError(err)

	s.celestiaCfg = dockerchain.DefaultConfig(s.client, s.network).WithTag(tag)
}

// make test-docker-e2e test=TestHyperlaneForwarding entrypoint=TestHyperlaneTestSuite
func (s *HyperlaneTestSuite) TestHyperlaneForwarding() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping hyperlane forwarding test in short mode")
	}

	ctx := context.Background()
	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), s.celestiaCfg).Build(ctx)
	s.Require().NoError(err)

	s.T().Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			s.T().Logf("Error removing chain: %v", err)
		}
	})

	err = chain.Start(ctx)
	s.Require().NoError(err)

	// create a da network, wiring up the provided chain using the CELESTIA_CUSTOM environment variable.
	da := s.DeployDANetwork(ctx, chain, s.client, s.network)

	reth0, _ := s.BuildEvolveEVMChain(ctx, s.BridgeNodeAddress(da), "reth0", 1234)
	reth1, _ := s.BuildEvolveEVMChain(ctx, s.BridgeNodeAddress(da), "reth1", 1235)

	d, err := hyperlane.NewDeployer(
		ctx,
		hyperlane.Config{
			Logger:          s.logger,
			DockerClient:    s.client,
			DockerNetworkID: s.network,
			HyperlaneImage:  hyperlane.DefaultDeployerImage(),
		},
		t.Name(),
		[]hyperlane.ChainConfigProvider{reth0, reth1, chain},
	)
	require.NoError(t, err)

	relayerBytes, err := d.ReadFile(ctx, "relayer-config.json")
	require.NoError(t, err)

	var relayerCfg hyperlane.RelayerConfig
	require.NoError(t, json.Unmarshal(relayerBytes, &relayerCfg))
	require.NotEmpty(t, relayerCfg.Chains["reth0"])
	require.NotEmpty(t, relayerCfg.Chains["reth1"])
	require.NotEmpty(t, relayerCfg.Chains[chain.Config.Name])

	require.NoError(t, d.Deploy(ctx))

	schema, err := d.GetOnDiskSchema(ctx)
	require.NoError(t, err)

	assertMailbox(t, ctx, schema, reth0, "reth0")
	assertMailbox(t, ctx, schema, reth1, "reth1")

	broadcaster := cosmos.NewBroadcaster(chain)
	faucet := chain.GetFaucetWallet()

	config, err := d.DeployCosmosNoopISM(ctx, broadcaster, faucet)
	require.NoError(t, err)
	require.NotNil(t, config)

	cosmosEntry, ok := schema.Registry.Chains[chain.Config.Name]
	require.True(t, ok, "missing registry entry for %s", chain.Config.Name)
	cosmosDomain := cosmosEntry.Metadata.DomainID

	networkInfo, err := chain.GetNetworkInfo(ctx)
	require.NoError(t, err)

	warpTokens, err := queryWarpTokens(ctx, networkInfo.External.GRPCAddress())
	require.NoError(t, err)
	require.NotEmpty(t, warpTokens)
	routerHex := warpTokens[0].Id

	tokenRouter, err := d.GetEVMWarpTokenAddress()
	require.NoError(t, err)

	enrollRemote := func(chainName string, node *reth.Node) {
		t.Helper()

		networkInfo, err := node.GetNetworkInfo(ctx)
		require.NoError(t, err)

		rpcURL := fmt.Sprintf("http://%s", networkInfo.External.RPCAddress())

		txHash, err := d.EnrollRemoteRouter(ctx, tokenRouter.Hex(), cosmosDomain, routerHex, chainName, rpcURL)
		require.NoError(t, err)
		t.Logf("Enrolled remote router for %s: %s", chainName, txHash.Hex())

		entry, ok := schema.Registry.Chains[chainName]
		require.True(t, ok, "missing registry entry for %s", chainName)
		evmDomain := entry.Metadata.DomainID

		remoteTokenRouter := evm.PadAddress(tokenRouter) // leftpad to bytes32
		require.NoError(t, d.EnrollRemoteRouterOnCosmos(ctx, broadcaster, faucet, config.TokenID, evmDomain, remoteTokenRouter.String()))
	}

	enrollRemote("reth0", reth0)
	enrollRemote("reth1", reth1)

}

func (s *HyperlaneTestSuite) BridgeNodeAddress(da *dataavailability.Network) string {
	s.T().Helper()

	networkInfo, err := da.GetBridgeNodes()[0].GetNetworkInfo(s.T().Context())
	require.NoError(s.T(), err)

	return fmt.Sprintf("http://%s:%s", networkInfo.Internal.IP, networkInfo.Internal.Ports.RPC)
}

func (s *HyperlaneTestSuite) BuildEvolveEVMChain(ctx context.Context, daAddress, chainName string, chainID int) (*reth.Node, *evmsingle.Chain) {
	s.T().Helper()
	t := s.T()

	rethNode, err := reth.NewNodeBuilderWithTestName(t, t.Name()).
		WithName(chainName).
		WithDockerClient(s.client).
		WithDockerNetworkID(s.network).
		WithGenesis([]byte(reth.DefaultEvolveGenesisJSON(reth.WithChainID(chainID)))).
		WithHyperlaneChainName(chainName).
		WithHyperlaneChainID(uint64(chainID)).
		WithHyperlaneDomainID(uint32(chainID)).
		Build(ctx)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = rethNode.Stop(ctx)
		_ = rethNode.Remove(ctx)
	})

	require.NoError(t, rethNode.Start(ctx))

	networkInfo, err := rethNode.GetNetworkInfo(ctx)
	require.NoError(t, err)

	genesisHash, err := rethNode.GenesisHash(ctx)
	require.NoError(t, err)

	evmEthURL := fmt.Sprintf("http://%s:%s", networkInfo.Internal.Hostname, networkInfo.Internal.Ports.RPC)
	evmEngineURL := fmt.Sprintf("http://%s:%s", networkInfo.Internal.Hostname, networkInfo.Internal.Ports.Engine)

	seqCfg := evmsingle.NewNodeConfigBuilder().
		WithEVMEngineURL(evmEngineURL).
		WithEVMETHURL(evmEthURL).
		WithEVMJWTSecret(rethNode.JWTSecretHex()).
		WithEVMSignerPassphrase("secret").
		WithEVMBlockTime("1s").
		WithEVMGenesisHash(genesisHash).
		WithDAAddress(daAddress).
		Build()

	seqNode, err := evmsingle.NewChainBuilderWithTestName(t, t.Name()).
		WithName(chainName).
		WithDockerClient(s.client).
		WithDockerNetworkID(s.network).
		WithNodes(seqCfg).
		Build(ctx)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = seqNode.Stop(ctx)
		_ = seqNode.Remove(ctx)
	})

	require.NoError(t, seqNode.Start(ctx))

	evmNodes := seqNode.Nodes()
	require.Len(t, evmNodes, 1)
	assertSeqLiveness(t, ctx, evmNodes[0])

	return rethNode, seqNode
}

// queryWarpTokens retrieves a list of wrapped hyperlane tokens from the specified gRPC address.
func queryWarpTokens(ctx context.Context, grpcAddr string) ([]warptypes.WrappedHypToken, error) {
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial grpc %s: %w", grpcAddr, err)
	}
	defer func() {
		_ = conn.Close()
	}()

	q := warptypes.NewQueryClient(conn)
	resp, err := q.Tokens(ctx, &warptypes.QueryTokensRequest{})
	if err != nil {
		return nil, fmt.Errorf("warp tokens query failed: %w", err)
	}
	return resp.Tokens, nil
}

func assertSeqLiveness(t *testing.T, ctx context.Context, node *evmsingle.Node) {
	t.Helper()
	networkInfo, err := node.GetNetworkInfo(ctx)
	require.NoError(t, err)

	healthcheck := fmt.Sprintf("http://0.0.0.0:%s/health/ready", networkInfo.External.Ports.RPC)
	require.Eventually(t, func() bool {
		req, _ := http.NewRequest(http.MethodGet, healthcheck, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode == http.StatusOK
	}, 60*time.Second, 2*time.Second, "evm sequencer %s failed to respond healthy", node.Name())
}

// func ensureFaucetKeyAlias(t *testing.T, ctx context.Context, chain *cosmos.Chain) {
// 	t.Helper()

// 	node := chain.GetNode()
// 	kr, err := node.GetKeyring()
// 	require.NoError(t, err)

// 	if _, err := kr.Key(tastoraconsts.FaucetAccountKeyName); err == nil {
// 		return
// 	}

// 	faucet := chain.GetFaucetWallet()
// 	require.NotNil(t, faucet, "faucet wallet not initialized")

// 	armoredKey, err := kr.ExportPrivKeyArmor(faucet.GetKeyName(), "")
// 	require.NoError(t, err, "failed to export faucet source key %s", faucet.GetKeyName())

// 	err = kr.ImportPrivKey(tastoraconsts.FaucetAccountKeyName, armoredKey, "")
// 	require.NoError(t, err, "failed to import faucet key alias")
// }

func assertMailbox(t *testing.T, ctx context.Context, schema *hyperlane.Schema, node *reth.Node, chainName string) {
	t.Helper()

	entry, ok := schema.Registry.Chains[chainName]
	require.True(t, ok, "missing registry entry for %s", chainName)

	mailbox := string(entry.Addresses.Mailbox)

	ethClient, err := node.GetEthClient(ctx)
	require.NoError(t, err)

	code, err := ethClient.CodeAt(ctx, gethcommon.HexToAddress(mailbox), nil)
	require.NoError(t, err, "failed to fetch code for mailbox")
	require.Greaterf(t, len(code), 0, "should have non-empty code")
}
