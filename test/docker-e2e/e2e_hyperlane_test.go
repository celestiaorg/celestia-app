package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"context"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	hyputil "github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	forwardingtypes "github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/docker/container"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/celestiaorg/tastora/framework/docker/dataavailability"
	"github.com/celestiaorg/tastora/framework/docker/evstack/evmsingle"
	"github.com/celestiaorg/tastora/framework/docker/evstack/reth"
	"github.com/celestiaorg/tastora/framework/docker/hyperlane"
	"github.com/celestiaorg/tastora/framework/testutil/evm"
	"github.com/celestiaorg/tastora/framework/testutil/query"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	// NOTE: This a workaround as using the chain name "celestia" causes configuration overlay issues
	// with the Hyperlane agents container. This can be reverted when the following issue is addressed.
	// See https://github.com/hyperlane-xyz/hyperlane-monorepo/issues/7598.
	HypCelestiaChainName = "celestiadev"
)

// EvolveEVMChain encapsulates both the evolve evm sequencer node and execution client node.
type EvolveEVMChain struct {
	*evmsingle.Chain
	*reth.Node
}

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

func (s *HyperlaneTestSuite) StartRelayerAgent(ctx context.Context, deployer *hyperlane.Deployer) {
	s.T().Helper()

	cfg := hyperlane.Config{
		Logger:          s.logger,
		DockerClient:    s.client,
		DockerNetworkID: s.network,
		HyperlaneImage:  container.NewImage("gcr.io/abacus-labs-dev/hyperlane-agent", "agents-v1.7.0", "1000:1000"),
	}

	agent, err := hyperlane.NewAgent(ctx, cfg, s.T().Name(), hyperlane.AgentTypeRelayer, deployer)
	s.Require().NoError(err)

	err = agent.Start(ctx)
	s.Require().NoError(err)

	s.T().Cleanup(func() {
		_ = agent.Stop(ctx)
		_ = agent.Remove(ctx)
	})
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

	reth0 := s.BuildEvolveEVMChain(ctx, s.BridgeNodeAddress(da), "reth0", 1234)
	reth1 := s.BuildEvolveEVMChain(ctx, s.BridgeNodeAddress(da), "reth1", 1235)

	hypConfig := hyperlane.Config{
		Logger:          s.logger,
		DockerClient:    s.client,
		DockerNetworkID: s.network,
		HyperlaneImage:  hyperlane.DefaultDeployerImage(),
	}

	hypChainProvider := []hyperlane.ChainConfigProvider{reth0, reth1, chain}
	hyp, err := hyperlane.NewDeployer(ctx, hypConfig, t.Name(), hypChainProvider)
	require.NoError(t, err)

	require.NoError(t, hyp.Deploy(ctx))

	broadcaster := cosmos.NewBroadcaster(chain)
	faucet := chain.GetFaucetWallet()

	config, err := hyp.DeployCosmosNoopISM(ctx, broadcaster, faucet)
	require.NoError(t, err)
	require.NotNil(t, config)

	tokenRouter, err := hyp.GetEVMWarpTokenAddress()
	require.NoError(t, err)

	schema, err := hyp.GetOnDiskSchema(ctx)
	require.NoError(t, err)

	s.EnrollRemoteRouters(ctx, chain, reth0, hyp, tokenRouter, config.TokenID)
	s.EnrollRemoteRouters(ctx, chain, reth1, hyp, tokenRouter, config.TokenID)

	s.StartRelayerAgent(ctx, hyp)

	networkInfo, err := chain.GetNetworkInfo(ctx)
	require.NoError(t, err)

	chainGRPC := networkInfo.External.GRPCAddress()
	cconn, err := grpc.NewClient(chainGRPC, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() {
		_ = cconn.Close()
	}()

	sendAmount0 := sdkmath.NewInt(1000)
	recipient := ethcommon.HexToAddress("0xaF9053bB6c4346381C77C2FeD279B17ABAfCDf4d")

	reth0Entry := schema.Registry.Chains["reth0"]
	_ = schema.Registry.Chains["reth1"]

	s.SendTransferRemoteTx(ctx, chain, config.TokenID, reth0Entry.Metadata.DomainID, recipient, sendAmount0)

	ethClient, err := reth0.GetEthClient(ctx)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		balance, err := evm.GetERC20Balance(ctx, ethClient, tokenRouter, recipient)
		if err != nil {
			t.Logf("reth0 balance query failed: %v", err)
			return false
		}
		t.Logf("reth0 recipient warp token balance: %s", balance.String())
		return balance.Cmp(sendAmount0.BigInt()) == 0
	}, 2*time.Minute, 5*time.Second, "reth0 recipient should receive minted warp tokens")

	forwardAddr := s.QueryForwardingAddress(ctx, chain, 1235, "0x0000000000000000000000004A60C46F671A3B86D78E9C0B793235C2D502D44E")
	t.Logf("forwarding recipient address: %s", forwardAddr)

	recipientAcc, err := sdk.AccAddressFromBech32(forwardAddr)
	require.NoError(t, err)

	forwardAddrBytes32, err := padBytes32(recipientAcc.Bytes())
	require.NoError(t, err)

	amount := sdkmath.NewInt(400)

	beforeBalance, err := query.Balance(ctx, cconn, forwardAddr, chain.Config.Denom)
	require.NoError(t, err)

	beforeEvmBalance, err := evm.GetERC20Balance(ctx, ethClient, tokenRouter, recipient)
	require.NoError(t, err)
	require.Truef(t, beforeEvmBalance.Cmp(amount.BigInt()) >= 0, "insufficient EVM balance for transferRemote: have %s want %s", beforeEvmBalance.String(), amount.String())

	celestiaDomain := s.GetDomainForChain(ctx, HypCelestiaChainName, hyp)
	s.SendTransferRemoteTxEvm(ctx, reth0, tokenRouter, celestiaDomain, forwardAddrBytes32, amount)

	expectedEvmBalance := new(big.Int).Sub(beforeEvmBalance, amount.BigInt())
	require.Eventually(t, func() bool {
		afterEvmBalance, err := evm.GetERC20Balance(ctx, ethClient, tokenRouter, recipient)
		if err != nil {
			t.Logf("reth0 balance query failed after transferRemote: %v", err)
			return false
		}
		return afterEvmBalance.Cmp(expectedEvmBalance) == 0
	}, 2*time.Minute, 5*time.Second, "EVM balance should decrease after transferRemote")

	require.Eventually(t, func() bool {
		afterBalance := s.QueryBankBalance(ctx, chain, forwardAddr, chain.Config.Denom)
		return afterBalance.Amount.Equal(beforeBalance.Add(amount))
	}, 2*time.Minute, 5*time.Second, "faucet balance should increase after transferRemote")

	quoteFee := s.QueryForwardingFee(ctx, chain, 1235)

	s.SendForwardingTx(ctx, chain, forwardAddr, 1235, "0x0000000000000000000000004A60C46F671A3B86D78E9C0B793235C2D502D44E", quoteFee)

	reth1Client, err := reth1.GetEthClient(ctx)
	require.NoError(t, err)
	destRecipientAddr := ethcommon.HexToAddress("0x0000000000000000000000004A60C46F671A3B86D78E9C0B793235C2D502D44E")

	beforeReth1Balance, err := evm.GetERC20Balance(ctx, reth1Client, tokenRouter, destRecipientAddr)
	require.NoError(t, err)

	expectedReth1Balance := new(big.Int).Add(beforeReth1Balance, amount.BigInt())
	require.Eventually(t, func() bool {
		afterReth1Balance, err := evm.GetERC20Balance(ctx, reth1Client, tokenRouter, destRecipientAddr)
		if err != nil {
			t.Logf("reth1 balance query failed: %v", err)
			return false
		}
		return afterReth1Balance.Cmp(expectedReth1Balance) == 0
	}, 2*time.Minute, 5*time.Second, "reth1 recipient should receive forwarded warp tokens")
}

func (s *HyperlaneTestSuite) BridgeNodeAddress(da *dataavailability.Network) string {
	s.T().Helper()

	networkInfo, err := da.GetBridgeNodes()[0].GetNetworkInfo(s.T().Context())
	require.NoError(s.T(), err)

	return fmt.Sprintf("http://%s:%s", networkInfo.Internal.IP, networkInfo.Internal.Ports.RPC)
}

func (s *HyperlaneTestSuite) BuildEvolveEVMChain(ctx context.Context, daAddress, chainName string, chainID int) *EvolveEVMChain {
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
	s.Require().NoError(err)

	t.Cleanup(func() {
		_ = rethNode.Stop(ctx)
		_ = rethNode.Remove(ctx)
	})

	err = rethNode.Start(ctx)
	s.Require().NoError(err)

	networkInfo, err := rethNode.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	genesisHash, err := rethNode.GenesisHash(ctx)
	s.Require().NoError(err)

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
	s.Require().NoError(err)

	t.Cleanup(func() {
		_ = seqNode.Stop(ctx)
		_ = seqNode.Remove(ctx)
	})

	err = seqNode.Start(ctx)
	s.Require().NoError(err)

	evmNodes := seqNode.Nodes()
	s.Require().Len(evmNodes, 1)

	waitForReady(t, ctx, evmNodes[0])

	return &EvolveEVMChain{seqNode, rethNode}
}

func (s *HyperlaneTestSuite) EnrollRemoteRouters(ctx context.Context, chain *cosmos.Chain, evolve *EvolveEVMChain, deployer *hyperlane.Deployer, tokenRouter ethcommon.Address, tokenID hyputil.HexAddress) {
	s.T().Helper()

	networkInfo, err := evolve.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	rpcURL := fmt.Sprintf("http://%s", networkInfo.External.RPCAddress())

	chainName := evolve.HyperlaneChainName()
	celestiaDomain := s.GetDomainForChain(ctx, HypCelestiaChainName, deployer)

	txHash, err := deployer.EnrollRemoteRouter(ctx, tokenRouter.Hex(), celestiaDomain, tokenID.String(), chainName, rpcURL)
	s.Require().NoError(err)
	s.Require().NotEmpty(txHash, "tx hash should be non-empty")

	broadcaster := cosmos.NewBroadcaster(chain)
	signer := chain.GetFaucetWallet()

	evmDomain := s.GetDomainForChain(ctx, chainName, deployer)
	remoteTokenRouter := evm.PadAddress(tokenRouter) // leftpad to bytes32
	err = deployer.EnrollRemoteRouterOnCosmos(ctx, broadcaster, signer, tokenID, evmDomain, remoteTokenRouter.String())
	s.Require().NoError(err)
}

func (s *HyperlaneTestSuite) GetDomainForChain(ctx context.Context, chainName string, deployer *hyperlane.Deployer) uint32 {
	s.T().Helper()

	schema, err := deployer.GetOnDiskSchema(ctx)
	s.Require().NoError(err)

	registryEntry, ok := schema.Registry.Chains[chainName]
	s.Require().True(ok)

	return registryEntry.Metadata.DomainID
}

func (s *HyperlaneTestSuite) QueryForwardingAddress(ctx context.Context, chain *cosmos.Chain, domain uint32, recipient string) string {
	s.T().Helper()

	networkInfo, err := chain.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	grpcAddress := networkInfo.External.GRPCAddress()
	grpcConn, err := grpc.NewClient(grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	s.Require().NoError(err)

	defer grpcConn.Close()

	req := forwardingtypes.QueryDeriveForwardingAddressRequest{
		DestDomain:    domain,
		DestRecipient: recipient,
	}

	client := forwardingtypes.NewQueryClient(grpcConn)
	resp, err := client.DeriveForwardingAddress(ctx, &req)
	s.Require().NoError(err)

	return resp.Address
}

func (s *HyperlaneTestSuite) QueryForwardingFee(ctx context.Context, chain *cosmos.Chain, destDomain uint32) sdk.Coin {
	s.T().Helper()

	networkInfo, err := chain.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	grpcAddress := networkInfo.External.GRPCAddress()
	grpcConn, err := grpc.NewClient(grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	s.Require().NoError(err)

	defer grpcConn.Close()

	req := &forwardingtypes.QueryQuoteForwardingFeeRequest{
		DestDomain: destDomain,
	}

	client := forwardingtypes.NewQueryClient(grpcConn)
	resp, err := client.QuoteForwardingFee(ctx, req)
	s.Require().NoError(err)

	return resp.Fee
}

func (s *HyperlaneTestSuite) QueryBankBalance(ctx context.Context, chain *cosmos.Chain, address, denom string) *sdk.Coin {
	s.T().Helper()

	networkInfo, err := chain.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	grpcAddress := networkInfo.External.GRPCAddress()
	grpcConn, err := grpc.NewClient(grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	s.Require().NoError(err)

	defer grpcConn.Close()

	req := &banktypes.QueryBalanceRequest{
		Address: address,
		Denom:   denom,
	}

	client := banktypes.NewQueryClient(grpcConn)
	resp, err := client.Balance(ctx, req)
	s.Require().NoError(err)

	return resp.Balance
}

func (s *HyperlaneTestSuite) QueryWarpTokens(ctx context.Context, chain *cosmos.Chain) []warptypes.WrappedHypToken {
	s.T().Helper()

	networkInfo, err := chain.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	grpcAddress := networkInfo.External.GRPCAddress()
	grpcConn, err := grpc.NewClient(grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	s.Require().NoError(err)

	defer grpcConn.Close()

	client := warptypes.NewQueryClient(grpcConn)
	resp, err := client.Tokens(ctx, &warptypes.QueryTokensRequest{})
	s.Require().NoError(err)

	return resp.Tokens
}

func (s *HyperlaneTestSuite) SendForwardingTx(ctx context.Context, chain *cosmos.Chain, forwardAddr string, destDomain uint32, destRecipient string, maxIgpFee sdk.Coin) {
	s.T().Helper()

	broadcaster := cosmos.NewBroadcaster(chain)
	signer := chain.GetFaucetWallet()

	msgForward := forwardingtypes.NewMsgForward(
		signer.GetFormattedAddress(),
		forwardAddr,
		destDomain,
		destRecipient,
		maxIgpFee,
	)

	resp, err := broadcaster.BroadcastMessages(ctx, signer, msgForward)
	s.Require().NoError(err)
	s.Require().Equal(resp.Code, uint32(0), "tx failed: code=%d, log=%s", resp.Code, resp.RawLog)
}

func (s *HyperlaneTestSuite) SendTransferRemoteTx(ctx context.Context, chain *cosmos.Chain, tokenID hyputil.HexAddress, domain uint32, recipient ethcommon.Address, amount sdkmath.Int) {
	s.T().Helper()

	broadcaster := cosmos.NewBroadcaster(chain)
	signer := chain.GetFaucetWallet()

	msgRemoteTransfer := &warptypes.MsgRemoteTransfer{
		Sender:            signer.GetFormattedAddress(),
		TokenId:           tokenID,
		DestinationDomain: domain,
		Recipient:         evm.PadAddress(recipient),
		Amount:            amount,
	}

	resp, err := broadcaster.BroadcastMessages(ctx, signer, msgRemoteTransfer)
	s.Require().NoError(err)
	s.Require().Equal(resp.Code, uint32(0), "tx failed: code=%d, log=%s", resp.Code, resp.RawLog)
}

func (s *HyperlaneTestSuite) SendTransferRemoteTxEvm(ctx context.Context, chain *EvolveEVMChain, tokenRouter ethcommon.Address, domain uint32, recipient [32]byte, amount sdkmath.Int) {
	s.T().Helper()

	// TODO: should provide a signer arg rather than relying on Hyperlane config defaults
	cfg, err := chain.GetHyperlaneRelayerChainConfig(ctx)
	s.Require().NoError(err)
	s.Require().NotNil(cfg.Signer)

	networkInfo, err := chain.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	sender, err := evm.NewSender(ctx, fmt.Sprintf("http://%s", networkInfo.External.RPCAddress()))
	s.Require().NoError(err)

	defer sender.Close()

	transferABI, err := parseTransferRemoteABI()
	s.Require().NoError(err)

	txHash, err := sender.SendFunctionTx(ctx,
		cfg.Signer.Key,
		tokenRouter.Hex(),
		transferABI,
		"transferRemote",
		domain,
		recipient,
		amount.BigInt(),
	)
	s.Require().NoError(err)
	s.Require().NotEmpty(txHash, "tx hash should be non-empty")
}

func waitForReady(t *testing.T, ctx context.Context, node *evmsingle.Node) {
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

func parseTransferRemoteABI() (abi.ABI, error) {
	return abi.JSON(strings.NewReader(`[
		{
			"inputs": [
				{"internalType": "uint32", "name": "_destination", "type": "uint32"},
				{"internalType": "bytes32", "name": "_recipient", "type": "bytes32"},
				{"internalType": "uint256", "name": "_amount", "type": "uint256"}
			],
			"name": "transferRemote",
			"outputs": [
				{"internalType": "bytes32", "name": "messageId", "type": "bytes32"}
			],
			"stateMutability": "payable",
			"type": "function"
		}
	]`))
}

func padBytes32(b []byte) ([32]byte, error) {
	if len(b) > 32 {
		return [32]byte{}, fmt.Errorf("recipient too long: %d bytes", len(b))
	}
	var out [32]byte
	copy(out[32-len(b):], b)
	return out, nil
}
