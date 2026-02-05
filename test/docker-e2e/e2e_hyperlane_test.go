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
	"github.com/ethereum/go-ethereum/accounts/abi"
	gethcommon "github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
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

	schema, err := hyp.GetOnDiskSchema(ctx)
	require.NoError(t, err)

	assertMailbox(t, ctx, schema, reth0, "reth0")
	assertMailbox(t, ctx, schema, reth1, "reth1")

	broadcaster := cosmos.NewBroadcaster(chain)
	faucet := chain.GetFaucetWallet()

	config, err := hyp.DeployCosmosNoopISM(ctx, broadcaster, faucet)
	require.NoError(t, err)
	require.NotNil(t, config)

	celestiaEntry, ok := schema.Registry.Chains[HypCelestiaChainName]
	require.True(t, ok, "missing registry entry for %s", HypCelestiaChainName)

	celestiaDomain := celestiaEntry.Metadata.DomainID

	networkInfo, err := chain.GetNetworkInfo(ctx)
	require.NoError(t, err)

	warpTokens, err := queryWarpTokens(ctx, networkInfo.External.GRPCAddress())
	require.NoError(t, err)
	require.NotEmpty(t, warpTokens)
	routerHex := warpTokens[0].Id

	tokenRouter, err := hyp.GetEVMWarpTokenAddress()
	require.NoError(t, err)

	enrollRemote := func(chainName string, node *EvolveEVMChain) {
		t.Helper()

		networkInfo, err := node.GetNetworkInfo(ctx)
		require.NoError(t, err)

		rpcURL := fmt.Sprintf("http://%s", networkInfo.External.RPCAddress())

		txHash, err := hyp.EnrollRemoteRouter(ctx, tokenRouter.Hex(), celestiaDomain, routerHex, chainName, rpcURL)
		require.NoError(t, err)
		t.Logf("Enrolled remote router for %s: %s", chainName, txHash.Hex())

		entry, ok := schema.Registry.Chains[chainName]
		require.True(t, ok, "missing registry entry for %s", chainName)
		evmDomain := entry.Metadata.DomainID

		remoteTokenRouter := evm.PadAddress(tokenRouter) // leftpad to bytes32
		require.NoError(t, hyp.EnrollRemoteRouterOnCosmos(ctx, broadcaster, faucet, config.TokenID, evmDomain, remoteTokenRouter.String()))
	}

	enrollRemote("reth0", reth0)
	enrollRemote("reth1", reth1)

	s.StartRelayerAgent(ctx, hyp)

	chainGRPC := networkInfo.External.GRPCAddress()
	cconn, err := grpc.NewClient(chainGRPC, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() {
		_ = cconn.Close()
	}()

	sendAmount0 := sdkmath.NewInt(1000)
	receiver := gethcommon.HexToAddress("0xaF9053bB6c4346381C77C2FeD279B17ABAfCDf4d")

	reth0Entry := schema.Registry.Chains["reth0"]
	_ = schema.Registry.Chains["reth1"]

	msgRemoteTransfer := &warptypes.MsgRemoteTransfer{
		Sender:            faucet.GetFormattedAddress(),
		TokenId:           config.TokenID,
		DestinationDomain: reth0Entry.Metadata.DomainID,
		Recipient:         evm.PadAddress(receiver),
		Amount:            sendAmount0,
	}
	resp, err := broadcaster.BroadcastMessages(ctx, faucet, msgRemoteTransfer)
	require.NoError(t, err)
	require.Equal(t, resp.Code, uint32(0), "reth0 transfer tx should succeed: code=%d, log=%s", resp.Code, resp.RawLog)

	ethClient, err := reth0.GetEthClient(ctx)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		balance, err := evm.GetERC20Balance(ctx, ethClient, tokenRouter, receiver)
		if err != nil {
			t.Logf("reth0 balance query failed: %v", err)
			return false
		}
		t.Logf("reth0 recipient warp token balance: %s", balance.String())
		return balance.Cmp(sendAmount0.BigInt()) == 0
	}, 2*time.Minute, 5*time.Second, "reth0 recipient should receive minted warp tokens")

	// transfer back to Celestia from EVM warp token router
	reth0Cfg, err := reth0.GetHyperlaneRelayerChainConfig(ctx)
	require.NoError(t, err)
	require.NotNil(t, reth0Cfg.Signer)

	reth0Net, err := reth0.GetNetworkInfo(ctx)
	require.NoError(t, err)
	evmRPC := fmt.Sprintf("http://%s", reth0Net.External.RPCAddress())

	transferABI, err := parseTransferRemoteABI()
	require.NoError(t, err)

	forwardAddr := s.QueryForwardingAddress(ctx, chain, 1235, "0x0000000000000000000000004A60C46F671A3B86D78E9C0B793235C2D502D44E")
	t.Logf("forwarding recipient address: %s", forwardAddr)

	recipientAcc, err := sdk.AccAddressFromBech32(forwardAddr)
	require.NoError(t, err)
	recipientBytes32, err := padBytes32(recipientAcc.Bytes())
	require.NoError(t, err)

	sendBack := sdkmath.NewInt(400)
	beforeBalance, err := query.Balance(ctx, cconn, forwardAddr, chain.Config.Denom)
	require.NoError(t, err)

	beforeEvmBalance, err := evm.GetERC20Balance(ctx, ethClient, tokenRouter, receiver)
	require.NoError(t, err)
	require.Truef(t, beforeEvmBalance.Cmp(sendBack.BigInt()) >= 0, "insufficient EVM balance for transferRemote: have %s want %s", beforeEvmBalance.String(), sendBack.String())

	sender, err := evm.NewSender(ctx, evmRPC)
	require.NoError(t, err)
	defer sender.Close()

	txHash, err := sender.SendFunctionTx(
		ctx,
		reth0Cfg.Signer.Key,
		tokenRouter.Hex(),
		transferABI,
		"transferRemote",
		celestiaDomain,
		recipientBytes32,
		sendBack.BigInt(),
	)
	require.NoError(t, err)
	t.Logf("transferRemote tx: %s", txHash.Hex())

	var receipt *gethtypes.Receipt
	require.Eventually(t, func() bool {
		r, err := ethClient.TransactionReceipt(ctx, txHash)
		if err != nil {
			return false
		}
		receipt = r
		return r.Status == 1
	}, 2*time.Minute, 5*time.Second, "transferRemote tx should succeed")

	mailboxAddr := gethcommon.HexToAddress(string(reth0Entry.Addresses.Mailbox))
	require.Truef(t, hasLogFromAddress(receipt.Logs, mailboxAddr), "transferRemote should emit mailbox dispatch log")
	for i, l := range receipt.Logs {
		t.Logf("\nlog[%d] addr=%s topics=%v data=0x%x", i, l.Address.Hex(), l.Topics, l.Data)
	}

	t.Log("mailbox has successfully outputted a dispatch log")

	expectedEvmBalance := new(big.Int).Sub(beforeEvmBalance, sendBack.BigInt())
	require.Eventually(t, func() bool {
		afterEvmBalance, err := evm.GetERC20Balance(ctx, ethClient, tokenRouter, receiver)
		if err != nil {
			t.Logf("reth0 balance query failed after transferRemote: %v", err)
			return false
		}
		return afterEvmBalance.Cmp(expectedEvmBalance) == 0
	}, 2*time.Minute, 5*time.Second, "EVM balance should decrease after transferRemote")
	t.Log("EVM withdrawal succeeded, waiting for relay...")

	fmt.Println("waiting for balance update...")
	require.Eventually(t, func() bool {
		afterBalance, err := query.Balance(ctx, cconn, forwardAddr, chain.Config.Denom)
		if err != nil {
			t.Logf("celestia balance query failed: %v", err)
			return false
		}
		return afterBalance.Equal(beforeBalance.Add(sendBack))
	}, 5*time.Minute, 5*time.Second, "faucet balance should increase after transferRemote")

	quoteFee := s.QueryForwardingFee(ctx, chain, 1235)

	s.SendForwardingTx(ctx, chain, forwardAddr, 1235, "0x0000000000000000000000004A60C46F671A3B86D78E9C0B793235C2D502D44E", quoteFee)

	reth1Client, err := reth1.GetEthClient(ctx)
	require.NoError(t, err)
	destRecipientAddr := gethcommon.HexToAddress("0x0000000000000000000000004A60C46F671A3B86D78E9C0B793235C2D502D44E")

	beforeReth1Balance, err := evm.GetERC20Balance(ctx, reth1Client, tokenRouter, destRecipientAddr)
	require.NoError(t, err)

	expectedReth1Balance := new(big.Int).Add(beforeReth1Balance, sendBack.BigInt())
	require.Eventually(t, func() bool {
		afterReth1Balance, err := evm.GetERC20Balance(ctx, reth1Client, tokenRouter, destRecipientAddr)
		if err != nil {
			t.Logf("reth1 balance query failed: %v", err)
			return false
		}
		return afterReth1Balance.Cmp(expectedReth1Balance) == 0
	}, 5*time.Minute, 5*time.Second, "reth1 recipient should receive forwarded warp tokens")
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

	return &EvolveEVMChain{seqNode, rethNode}
}

func (s *HyperlaneTestSuite) QueryForwardingAddress(ctx context.Context, chain *cosmos.Chain, domain uint32, recipient string) string {
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
	networkInfo, err := chain.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	grpcAddress := networkInfo.External.GRPCAddress()
	grpcConn, err := grpc.NewClient(grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	s.Require().NoError(err)

	defer grpcConn.Close()

	client := forwardingtypes.NewQueryClient(grpcConn)
	resp, err := client.QuoteForwardingFee(ctx, &forwardingtypes.QueryQuoteForwardingFeeRequest{
		DestDomain: destDomain,
	})
	s.Require().NoError(err)

	return resp.Fee
}

func (s *HyperlaneTestSuite) SendForwardingTx(ctx context.Context, chain *cosmos.Chain, forwardAddr string, destDomain uint32, destRecipient string, maxIgpFee sdk.Coin) {
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

func hasLogFromAddress(logs []*gethtypes.Log, addr gethcommon.Address) bool {
	for _, l := range logs {
		if l.Address == addr {
			return true
		}
	}
	return false
}

func assertMailbox(t *testing.T, ctx context.Context, schema *hyperlane.Schema, node *EvolveEVMChain, chainName string) {
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
