package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"context"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	hyputil "github.com/bcp-innovations/hyperlane-cosmos/util"
	pdtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/types"
	"github.com/celestiaorg/tastora/framework/docker/container"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/celestiaorg/tastora/framework/docker/hyperlane"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// forwardingRelayerImage resolves the forwarding-relayer image. It is overridable via
// FORWARDING_RELAYER_IMAGE=repo:tag so the custom-hook e2e can run against a branch build
// (e.g. ghcr.io/celestiaorg/forwarding-relayer:custom-igp-hook) that supports CUSTOM_IGP_HOOK.
func forwardingRelayerImage() container.Image {
	if v := os.Getenv("FORWARDING_RELAYER_IMAGE"); v != "" {
		if repo, tag, ok := strings.Cut(v, ":"); ok {
			return container.NewImage(repo, tag, "0:0")
		}
	}
	return ForwardingRelayerImage
}

// TestHyperlaneForwardingCustomHook is the real-relaying proof of the x/forwarding
// custom_hook_id change: a forward routed through the patched forwarding-relayer
// (configured with CUSTOM_IGP_HOOK = our IGP) pays OUR IGP on Celestia — not the
// mailbox default hook — and is still delivered on the destination EVM chain by the
// Hyperlane relayer agent. Requires the patched celestia image (CELESTIA_TAG) and the
// patched forwarding-relayer image (FORWARDING_RELAYER_IMAGE).
func (s *HyperlaneTestSuite) TestHyperlaneForwardingCustomHook() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping hyperlane custom-hook forwarding test in short mode")
	}

	ctx := context.Background()
	cfg := dockerchain.DefaultConfig(s.client, s.network).WithTag(s.celestiaCfg.Tag)

	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).Build(ctx)
	s.Require().NoError(err)
	s.T().Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			s.T().Logf("Error removing chain: %v", err)
		}
	})
	s.Require().NoError(chain.Start(ctx))

	da := s.WithBridgeNodeNetwork(ctx, chain)
	reth0 := s.BuildEvolveEVMChain(ctx, s.BridgeNodeAddress(da), RethChainName0, RethChainID0)
	reth1 := s.BuildEvolveEVMChain(ctx, s.BridgeNodeAddress(da), RethChainName1, RethChainID1)

	hypConfig := hyperlane.Config{
		Logger:          s.logger,
		DockerClient:    s.client,
		DockerNetworkID: s.network,
		HyperlaneImage:  hyperlane.DefaultDeployerImage(),
	}
	hyp, err := hyperlane.NewDeployer(ctx, hypConfig, t.Name(), []hyperlane.ChainConfigProvider{reth0, reth1, chain})
	s.Require().NoError(err)
	s.Require().NoError(hyp.Deploy(ctx))

	broadcaster := cosmos.NewBroadcaster(chain)
	faucet := chain.GetFaucetWallet()

	config, err := hyp.DeployCosmosNoopISM(ctx, broadcaster, faucet)
	s.Require().NoError(err)
	s.Require().NotNil(config)

	tokenRouter, err := hyp.GetEVMWarpTokenAddress()
	s.Require().NoError(err)

	s.EnrollRemoteRouters(ctx, chain, reth0, hyp, tokenRouter, config.TokenID)
	s.EnrollRemoteRouters(ctx, chain, reth1, hyp, tokenRouter, config.TokenID)

	s.StartRelayerAgent(ctx, hyp)

	destDomain := s.GetDomainForChain(ctx, reth1.HyperlaneChainName(), hyp)

	// Deploy OUR IGP on Celestia and price it for the destination domain so a forward
	// routed through it quotes (and pays) a positive fee to this IGP.
	ourIGP := s.createCustomIGP(ctx, chain, destDomain)

	// Initial deposit celestia -> reth0 to seed the collateral on the EVM side.
	initialDeposit := sdkmath.NewInt(1000)
	recipient := ethcommon.HexToAddress("0xaF9053bB6c4346381C77C2FeD279B17ABAfCDf4d")
	domain0 := s.GetDomainForChain(ctx, reth0.HyperlaneChainName(), hyp)
	s.SendTransferRemoteTx(ctx, chain, config.TokenID, domain0, recipient, initialDeposit)
	s.AssertERC20Balance(ctx, reth0, tokenRouter, recipient, initialDeposit.BigInt())

	// The forwarding-relayer is configured to attach our IGP as the forward's hook.
	forwardingService := s.ConfigureForwardRelayer(ctx, chain, []string{
		fmt.Sprintf("CUSTOM_IGP_HOOK=%s", ourIGP.String()),
	})

	destRecipient := "0x0000000000000000000000004A60C46F671A3B86D78E9C0B793235C2D502D44E"
	forwardAddress := s.QueryForwardingAddress(ctx, chain, config.TokenID.String(), destDomain, destRecipient)
	s.SendForwardingRequest(ctx, forwardingService, forwardAddress, config.TokenID.String(), destDomain, destRecipient)

	forwardAddrBytes32, err := bech32ToBytes(forwardAddress)
	s.Require().NoError(err)
	beforeForwardBalance := s.QueryBankBalance(ctx, chain, forwardAddress, chain.Config.Denom)

	// Trigger the reth0 -> reth1 transfer via celestia x/forwarding.
	amount := sdkmath.NewInt(500)
	celestiaDomain := s.GetDomainForChain(ctx, HypCelestiaChainName, hyp)
	s.SendTransferRemoteTxEvm(ctx, reth0, tokenRouter, celestiaDomain, forwardAddrBytes32, amount)

	expForwardBalance := beforeForwardBalance.Add(amount)
	s.AssertBankBalance(ctx, chain, forwardAddress, chain.Config.Denom, expForwardBalance)

	// Delivery on reth1 by the real relayer agent (the forward paid OUR IGP, so a relayer
	// watching it delivers). Same assertion as the baseline forwarding test.
	destRecipientAddress := ethcommon.HexToAddress(destRecipient)
	balanceBefore := s.QueryERC20Balance(ctx, reth1, tokenRouter, destRecipientAddress)
	expectedBalance := new(big.Int).Add(balanceBefore, amount.BigInt())
	s.AssertERC20Balance(ctx, reth1, tokenRouter, destRecipientAddress, expectedBalance)

	// The decisive assertion: the forward's interchain gas payment landed in OUR IGP,
	// proving custom_hook_id routed the fee to the chosen hook rather than the default.
	claimable := s.queryIgpClaimable(ctx, chain, ourIGP)
	s.Require().Falsef(claimable.Empty(), "custom IGP %s should have collected the forward's gas payment", ourIGP.String())
	s.T().Logf("custom IGP %s collected fees: %s", ourIGP.String(), claimable.String())
}

// createCustomIGP creates an IGP owned by the chain faucet and prices it for destDomain.
func (s *HyperlaneTestSuite) createCustomIGP(ctx context.Context, chain *cosmos.Chain, destDomain uint32) hyputil.HexAddress {
	s.T().Helper()
	broadcaster := cosmos.NewBroadcaster(chain)
	signer := chain.GetFaucetWallet()

	resp, err := broadcaster.BroadcastMessages(ctx, signer, &pdtypes.MsgCreateIgp{
		Owner: signer.GetFormattedAddress(),
		Denom: chain.Config.Denom,
	})
	s.Require().NoError(err)
	s.Require().Equalf(uint32(0), resp.Code, "create igp failed: code=%d log=%s", resp.Code, resp.RawLog)

	igpID := hyputil.NewZeroAddress()
	for _, evt := range resp.Events {
		if evt.GetType() != proto.MessageName(&pdtypes.EventCreateIgp{}) {
			continue
		}
		typed, err := sdk.ParseTypedEvent(evt)
		s.Require().NoError(err)
		e, ok := typed.(*pdtypes.EventCreateIgp)
		s.Require().True(ok)
		igpID = e.IgpId
	}
	s.Require().NotEqual(hyputil.NewZeroAddress(), igpID, "EventCreateIgp not found in tx events")

	// Price the destination domain: fee = gasOverhead * gasPrice * exchRate / 1e10.
	resp, err = broadcaster.BroadcastMessages(ctx, signer, &pdtypes.MsgSetDestinationGasConfig{
		Owner: signer.GetFormattedAddress(),
		IgpId: igpID,
		DestinationGasConfig: &pdtypes.DestinationGasConfig{
			RemoteDomain: destDomain,
			GasOracle:    &pdtypes.GasOracle{TokenExchangeRate: sdkmath.NewInt(1), GasPrice: sdkmath.NewInt(10_000_000_000)},
			GasOverhead:  sdkmath.NewInt(200000),
		},
	})
	s.Require().NoError(err)
	s.Require().Equalf(uint32(0), resp.Code, "set destination gas config failed: code=%d log=%s", resp.Code, resp.RawLog)

	return igpID
}

// queryIgpClaimable returns the claimable fees accrued by an IGP (proof of which IGP was paid).
func (s *HyperlaneTestSuite) queryIgpClaimable(ctx context.Context, chain *cosmos.Chain, igpID hyputil.HexAddress) sdk.Coins {
	s.T().Helper()
	networkInfo, err := chain.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	grpcConn, err := grpc.NewClient(networkInfo.External.GRPCAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	s.Require().NoError(err)
	defer grpcConn.Close()

	client := pdtypes.NewQueryClient(grpcConn)
	resp, err := client.Igp(ctx, &pdtypes.QueryIgpRequest{Id: igpID.String()})
	s.Require().NoError(err)
	return resp.Igp.ClaimableFees
}
