package interop

import (
	"encoding/hex"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v7/app/params"
	forwardingtypes "github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	minttypes "github.com/celestiaorg/celestia-app/v7/x/mint/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/stretchr/testify/suite"
)

type ForwardingIntegrationTestSuite struct {
	HyperlaneTestSuite
	chainB *ibctesting.TestChain
}

func TestForwardingIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(ForwardingIntegrationTestSuite))
}

func (s *ForwardingIntegrationTestSuite) SetupTest() {
	_, celestia, chainA, chainB := SetupTest(s.T())

	s.celestia = celestia
	s.chainA = chainA
	s.chainB = chainB

	app := s.GetCelestiaApp(celestia)
	coins := sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(10_000_000)))

	err := app.BankKeeper.MintCoins(celestia.GetContext(), minttypes.ModuleName, coins)
	s.Require().NoError(err)

	err = app.BankKeeper.SendCoinsFromModuleToAccount(celestia.GetContext(), minttypes.ModuleName, celestia.SenderAccount.GetAddress(), coins)
	s.Require().NoError(err)
}

func (s *ForwardingIntegrationTestSuite) extractDispatchMessage(events []abci.Event) string {
	for _, evt := range events {
		if evt.Type == proto.MessageName(&coretypes.EventDispatch{}) {
			protoMsg, err := sdk.ParseTypedEvent(evt)
			if err != nil {
				continue
			}
			if eventDispatch, ok := protoMsg.(*coretypes.EventDispatch); ok {
				return eventDispatch.Message
			}
		}
	}
	return ""
}

func (s *ForwardingIntegrationTestSuite) countDispatchEvents(events []abci.Event) int {
	count := 0
	for _, evt := range events {
		if evt.Type == proto.MessageName(&coretypes.EventDispatch{}) {
			count++
		}
	}
	return count
}

func makeRecipient32(addr sdk.AccAddress) []byte {
	recipient := make([]byte, 32)
	copy(recipient[12:], addr.Bytes())
	return recipient
}

func recipientToHex(recipient []byte) util.HexAddress {
	hexAddr, _ := util.DecodeHexAddress("0x" + hex.EncodeToString(recipient))
	return hexAddr
}

func (s *ForwardingIntegrationTestSuite) fundAddress(chain *ibctesting.TestChain, addr sdk.AccAddress, coin sdk.Coin) {
	ctx := chain.GetContext()
	app := s.GetCelestiaApp(chain)

	err := app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, sdk.NewCoins(coin))
	s.Require().NoError(err)

	err = app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, addr, sdk.NewCoins(coin))
	s.Require().NoError(err)
}

func (s *ForwardingIntegrationTestSuite) processWarpMessage(
	srcChain *ibctesting.TestChain,
	dstChain *ibctesting.TestChain,
	dstMailboxID util.HexAddress,
	msg *warptypes.MsgRemoteTransfer,
) {
	res, err := srcChain.SendMsgs(msg)
	s.Require().NoError(err)

	hypMsg := s.extractDispatchMessage(res.Events)
	s.Require().NotEmpty(hypMsg, "should have hyperlane dispatch message")

	_, err = dstChain.SendMsgs(&coretypes.MsgProcessMessage{
		MailboxId: dstMailboxID,
		Relayer:   dstChain.SenderAccount.GetAddress().String(),
		Message:   hypMsg,
	})
	s.Require().NoError(err)
}

func (s *ForwardingIntegrationTestSuite) TestParamsStorageWithProtoTypes() {
	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set params with min forward amount
	newParams := forwardingtypes.NewParams(math.NewInt(100))

	err := celestiaApp.ForwardingKeeper.SetParams(ctx, newParams)
	s.Require().NoError(err)

	// Get params back
	retrievedParams, err := celestiaApp.ForwardingKeeper.GetParams(ctx)
	s.Require().NoError(err)

	s.Equal(math.NewInt(100), retrievedParams.MinForwardAmount)

	s.T().Logf("Test 1 PASSED: Params storage works with proto-generated types")
	s.T().Logf("MinForwardAmount: %s", retrievedParams.MinForwardAmount.String())
}

func (s *ForwardingIntegrationTestSuite) TestFindHypTokenByDenom_TIA() {
	const (
		CelestiaDomainID = 69420
		ChainADomainID   = 1337
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set up hyperlane infrastructure
	ismID := s.SetupNoopISM(s.celestia)
	mailboxID := s.SetupMailBox(s.celestia, ismID, CelestiaDomainID)

	// Create a collateral token for utia (TIA)
	collatTokenID := s.CreateCollateralToken(s.celestia, ismID, mailboxID, params.BondDenom)

	// Set up simapp counterparty and enroll router so TIA has a route
	ismIDSimapp := s.SetupNoopISM(s.chainA)
	s.SetupMailBox(s.chainA, ismIDSimapp, ChainADomainID)
	tiaSynTokenID := s.CreateSyntheticToken(s.chainA, ismIDSimapp, mailboxID)
	s.EnrollRemoteRouter(s.celestia, collatTokenID, ChainADomainID, tiaSynTokenID.String())

	// Test FindHypTokenByDenom for "utia" with destDomain
	hypToken, err := celestiaApp.ForwardingKeeper.FindHypTokenByDenom(ctx, "utia", ChainADomainID)
	s.Require().NoError(err)

	s.Equal(warptypes.HYP_TOKEN_TYPE_COLLATERAL, hypToken.TokenType)
	s.Equal(params.BondDenom, hypToken.OriginDenom)

	s.T().Logf("Test 2 PASSED: FindHypTokenByDenom works for TIA collateral")
	s.T().Logf("Token type: %v", hypToken.TokenType)
	s.T().Logf("Origin denom: %s", hypToken.OriginDenom)
}

func (s *ForwardingIntegrationTestSuite) TestFindHypTokenByDenom_Synthetic() {
	const (
		CelestiaDomainID = 69420
		ChainADomainID   = 1337
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set up hyperlane infrastructure on simapp (origin chain)
	ismIDSimapp := s.SetupNoopISM(s.chainA)
	mailboxIDSimapp := s.SetupMailBox(s.chainA, ismIDSimapp, ChainADomainID)

	// Set up celestia
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)

	// Create a synthetic token on celestia (representing a token from simapp)
	synTokenID := s.CreateSyntheticToken(s.celestia, ismIDCelestia, mailboxIDSimapp)

	// Get the synthetic token to find its denom
	hypToken, err := celestiaApp.WarpKeeper.HypTokens.Get(ctx, synTokenID.GetInternalId())
	s.Require().NoError(err)

	syntheticDenom := hypToken.OriginDenom
	s.T().Logf("Synthetic denom: %s", syntheticDenom)

	// Enroll router so the synthetic has a route to ChainADomainID
	s.EnrollRemoteRouter(s.celestia, synTokenID, ChainADomainID, "0x0000000000000000000000000000000000000000000000000000000000000001")

	// Test FindHypTokenByDenom for synthetic denom
	foundToken, err := celestiaApp.ForwardingKeeper.FindHypTokenByDenom(ctx, syntheticDenom, ChainADomainID)
	s.Require().NoError(err)

	s.Equal(warptypes.HYP_TOKEN_TYPE_SYNTHETIC, foundToken.TokenType)
	s.Equal(syntheticDenom, foundToken.OriginDenom)

	s.T().Logf("Test 3 PASSED: FindHypTokenByDenom works for synthetic tokens")
	s.T().Logf("Token type: %v", foundToken.TokenType)
}

func (s *ForwardingIntegrationTestSuite) TestHasEnrolledRouter() {
	const (
		CelestiaDomainID uint32 = 69420
		ChainADomainID   uint32 = 1337
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set up hyperlane infrastructure
	ismID := s.SetupNoopISM(s.celestia)
	mailboxID := s.SetupMailBox(s.celestia, ismID, CelestiaDomainID)
	collatTokenID := s.CreateCollateralToken(s.celestia, ismID, mailboxID, params.BondDenom)

	// Before enrollment - should return false
	hasRouteBefore, err := celestiaApp.ForwardingKeeper.HasEnrolledRouter(ctx, collatTokenID, ChainADomainID)
	s.Require().NoError(err)
	s.False(hasRouteBefore, "should NOT have enrolled router before enrollment")

	// Set up simapp and enroll router
	ismIDSimapp := s.SetupNoopISM(s.chainA)
	s.SetupMailBox(s.chainA, ismIDSimapp, ChainADomainID)
	synTokenID := s.CreateSyntheticToken(s.chainA, ismIDSimapp, mailboxID)
	s.EnrollRemoteRouter(s.celestia, collatTokenID, ChainADomainID, synTokenID.String())

	// After enrollment - should return true
	hasRouteAfter, err := celestiaApp.ForwardingKeeper.HasEnrolledRouter(ctx, collatTokenID, ChainADomainID)
	s.Require().NoError(err)
	s.True(hasRouteAfter, "should have enrolled router after enrollment")

	// Non-existent domain - should return false
	hasNonExistent, err := celestiaApp.ForwardingKeeper.HasEnrolledRouter(ctx, collatTokenID, 99999)
	s.Require().NoError(err)
	s.False(hasNonExistent, "should NOT have router for non-existent domain")

	s.T().Logf("Test 4 PASSED: HasEnrolledRouter pre-check works correctly")
	s.T().Logf("Before enrollment: %v", hasRouteBefore)
	s.T().Logf("After enrollment: %v", hasRouteAfter)
	s.T().Logf("Non-existent domain: %v", hasNonExistent)
}

func (s *ForwardingIntegrationTestSuite) TestHasAnyRouteToDestination() {
	const (
		CelestiaDomainID uint32 = 69420
		ChainADomainID   uint32 = 1337
		UnknownDomainID  uint32 = 99999
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Test 1: No routes exist yet
	hasRoute, err := celestiaApp.ForwardingKeeper.HasAnyRouteToDestination(ctx, ChainADomainID)
	s.Require().NoError(err)
	s.False(hasRoute, "should return false when no routes exist")

	// Setup infrastructure
	ismID := s.SetupNoopISM(s.celestia)
	mailboxID := s.SetupMailBox(s.celestia, ismID, CelestiaDomainID)
	collatTokenID := s.CreateCollateralToken(s.celestia, ismID, mailboxID, params.BondDenom)

	// Test 2: Token exists but no route enrolled
	hasRoute, err = celestiaApp.ForwardingKeeper.HasAnyRouteToDestination(ctx, ChainADomainID)
	s.Require().NoError(err)
	s.False(hasRoute, "should return false when token exists but no route enrolled")

	// Enroll route for collateral token
	ismIDSimapp := s.SetupNoopISM(s.chainA)
	s.SetupMailBox(s.chainA, ismIDSimapp, ChainADomainID)
	synTokenID := s.CreateSyntheticToken(s.chainA, ismIDSimapp, mailboxID)
	s.EnrollRemoteRouter(s.celestia, collatTokenID, ChainADomainID, synTokenID.String())

	// Test 3: Collateral token has route - returns true
	hasRoute, err = celestiaApp.ForwardingKeeper.HasAnyRouteToDestination(ctx, ChainADomainID)
	s.Require().NoError(err)
	s.True(hasRoute, "should return true when collateral token has route")

	// Test 4: Non-existent domain - returns false
	hasRoute, err = celestiaApp.ForwardingKeeper.HasAnyRouteToDestination(ctx, UnknownDomainID)
	s.Require().NoError(err)
	s.False(hasRoute, "should return false for non-existent domain")

	s.T().Logf("Test PASSED: HasAnyRouteToDestination works correctly")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForward_FullFlow() {
	const (
		CelestiaDomainID uint32 = 69420
		ChainADomainID   uint32 = 1337
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set up hyperlane infrastructure
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)
	collatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)

	// Set up simapp counterparty
	ismIDSimapp := s.SetupNoopISM(s.chainA)
	mailboxIDSimapp := s.SetupMailBox(s.chainA, ismIDSimapp, ChainADomainID)
	synTokenID := s.CreateSyntheticToken(s.chainA, ismIDSimapp, mailboxIDCelestia)

	// Enroll routers
	s.EnrollRemoteRouter(s.celestia, collatTokenID, ChainADomainID, synTokenID.String())
	s.EnrollRemoteRouter(s.chainA, synTokenID, CelestiaDomainID, collatTokenID.String())

	// Create destination recipient and derive forwarding address
	destRecipient := makeRecipient32(s.chainA.SenderAccount.GetAddress())
	forwardAddr, err := forwardingtypes.DeriveForwardingAddress(ChainADomainID, destRecipient)
	s.Require().NoError(err)

	// Fund the forwarding address
	fundAmount := math.NewInt(1000)
	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(params.BondDenom, fundAmount))

	// Verify forward address has funds
	forwardBalance := celestiaApp.BankKeeper.GetBalance(ctx, forwardAddr, params.BondDenom)
	s.Equal(fundAmount.Int64(), forwardBalance.Amount.Int64())

	// Create and execute MsgForward
	msg := forwardingtypes.NewMsgForward(
		s.celestia.SenderAccount.GetAddress().String(),
		sdk.AccAddress(forwardAddr).String(),
		ChainADomainID,
		recipientToHex(destRecipient).String(),
		sdk.NewCoin("utia", math.NewInt(0)), // IGP fee (0 for noop ISM)
	)

	res, err := s.celestia.SendMsgs(msg)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	// Verify forward address is now empty
	newForwardBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	s.True(newForwardBalance.Amount.IsZero(), "forward address should be empty after forwarding")

	// If we found a dispatch event, verify the message can be processed on simapp
	hypMsg := s.extractDispatchMessage(res.Events)
	if hypMsg != "" {
		res, err = s.chainA.SendMsgs(&coretypes.MsgProcessMessage{
			MailboxId: mailboxIDSimapp,
			Relayer:   s.chainA.SenderAccount.GetAddress().String(),
			Message:   hypMsg,
		})
		if err == nil {
			s.Require().NotNil(res)

			// Verify tokens arrived at destination
			simapp := s.GetSimapp(s.chainA)
			hypDenom, err := simapp.WarpKeeper.HypTokens.Get(s.chainA.GetContext(), synTokenID.GetInternalId())
			s.Require().NoError(err)

			destBalance := simapp.BankKeeper.GetBalance(s.chainA.GetContext(), s.chainA.SenderAccount.GetAddress(), hypDenom.OriginDenom)
			s.Equal(fundAmount.Int64(), destBalance.Amount.Int64())
		}
	}
}

func (s *ForwardingIntegrationTestSuite) TestMsgForward_AddressMismatch() {
	randomAddr := sdk.AccAddress([]byte("random_address______"))
	destRecipient := makeRecipient32(s.chainA.SenderAccount.GetAddress())

	msg := forwardingtypes.NewMsgForward(
		s.celestia.SenderAccount.GetAddress().String(),
		randomAddr.String(),
		1337,
		recipientToHex(destRecipient).String(),
		sdk.NewCoin("utia", math.NewInt(0)), // IGP fee (0 for noop ISM)
	)

	_, err := s.celestia.SendMsgs(msg)
	s.Require().Error(err)
	s.Contains(err.Error(), "derived address does not match")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForward_NoBalance() {
	destRecipient := makeRecipient32(s.chainA.SenderAccount.GetAddress())
	forwardAddr, err := forwardingtypes.DeriveForwardingAddress(1337, destRecipient)
	s.Require().NoError(err)

	msg := forwardingtypes.NewMsgForward(
		s.celestia.SenderAccount.GetAddress().String(),
		sdk.AccAddress(forwardAddr).String(),
		1337,
		recipientToHex(destRecipient).String(),
		sdk.NewCoin("utia", math.NewInt(0)), // IGP fee (0 for noop ISM)
	)

	_, err = s.celestia.SendMsgs(msg)
	s.Require().Error(err)
	s.Contains(err.Error(), "no balance")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForward_MultiToken() {
	const (
		CelestiaDomainID uint32 = 69420
		ChainADomainID   uint32 = 1337
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)

	// Set up hyperlane infrastructure on Celestia
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)
	tiaCollatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)

	// Set up simapp counterparty
	ismIDSimapp := s.SetupNoopISM(s.chainA)
	mailboxIDSimapp := s.SetupMailBox(s.chainA, ismIDSimapp, ChainADomainID)
	tiaSynTokenID := s.CreateSyntheticToken(s.chainA, ismIDSimapp, mailboxIDCelestia)

	// Create second token pair: simapp collateral -> Celestia synthetic
	simappCollatTokenID := s.CreateCollateralToken(s.chainA, ismIDSimapp, mailboxIDSimapp, sdk.DefaultBondDenom)
	celestiaSynTokenID := s.CreateSyntheticToken(s.celestia, ismIDCelestia, mailboxIDSimapp)

	// Enroll routers for both token pairs
	s.EnrollRemoteRouter(s.celestia, tiaCollatTokenID, ChainADomainID, tiaSynTokenID.String())
	s.EnrollRemoteRouter(s.chainA, tiaSynTokenID, CelestiaDomainID, tiaCollatTokenID.String())
	s.EnrollRemoteRouter(s.chainA, simappCollatTokenID, CelestiaDomainID, celestiaSynTokenID.String())
	s.EnrollRemoteRouter(s.celestia, celestiaSynTokenID, ChainADomainID, simappCollatTokenID.String())

	// Create destination recipient and derive forwarding address
	destRecipient := makeRecipient32(s.chainA.SenderAccount.GetAddress())
	forwardAddr, err := forwardingtypes.DeriveForwardingAddress(ChainADomainID, destRecipient)
	s.Require().NoError(err)

	// Fund forward address with TIA
	tiaAmount := math.NewInt(1000)
	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(params.BondDenom, tiaAmount))

	// Warp transfer stake from simapp to forwardAddr on Celestia
	forwardAddrBytes := makeRecipient32(forwardAddr)
	s.processWarpMessage(s.chainA, s.celestia, mailboxIDCelestia, &warptypes.MsgRemoteTransfer{
		Sender:            s.chainA.SenderAccount.GetAddress().String(),
		TokenId:           simappCollatTokenID,
		DestinationDomain: CelestiaDomainID,
		Recipient:         util.HexAddress(forwardAddrBytes),
		Amount:            math.NewInt(500),
	})

	// Get synthetic token denom
	synToken, err := celestiaApp.WarpKeeper.HypTokens.Get(s.celestia.GetContext(), celestiaSynTokenID.GetInternalId())
	s.Require().NoError(err)
	syntheticDenom := synToken.OriginDenom

	// Verify forward address has both tokens
	tiaBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	synBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, syntheticDenom)
	s.Equal(tiaAmount.Int64(), tiaBalance.Amount.Int64())
	s.Equal(int64(500), synBalance.Amount.Int64())

	// Execute forwarding
	msg := forwardingtypes.NewMsgForward(
		s.celestia.SenderAccount.GetAddress().String(),
		sdk.AccAddress(forwardAddr).String(),
		ChainADomainID,
		recipientToHex(destRecipient).String(),
		sdk.NewCoin("utia", math.NewInt(0)), // IGP fee (0 for noop ISM)
	)

	res, err := s.celestia.SendMsgs(msg)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	// Verify forward address is now empty for BOTH tokens
	newTiaBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	newSynBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, syntheticDenom)
	s.True(newTiaBalance.Amount.IsZero(), "TIA balance should be zero after forwarding")
	s.True(newSynBalance.Amount.IsZero(), "SYNTHETIC balance should be zero after forwarding")

	// Verify 2 dispatch events (one per token)
	s.Equal(2, s.countDispatchEvents(res.Events), "should have 2 dispatch events for 2 tokens")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForward_PartialFailure_UnsupportedToken() {
	const (
		CelestiaDomainID uint32 = 69420
		ChainADomainID   uint32 = 1337
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)

	// Set up hyperlane infrastructure
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)
	tiaCollatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)

	// Set up simapp counterparty
	ismIDSimapp := s.SetupNoopISM(s.chainA)
	_ = s.SetupMailBox(s.chainA, ismIDSimapp, ChainADomainID)
	tiaSynTokenID := s.CreateSyntheticToken(s.chainA, ismIDSimapp, mailboxIDCelestia)

	// Enroll routers for TIA only
	s.EnrollRemoteRouter(s.celestia, tiaCollatTokenID, ChainADomainID, tiaSynTokenID.String())
	s.EnrollRemoteRouter(s.chainA, tiaSynTokenID, CelestiaDomainID, tiaCollatTokenID.String())

	// Create destination and derive forwarding address
	destRecipient := makeRecipient32(s.chainA.SenderAccount.GetAddress())
	forwardAddr, err := forwardingtypes.DeriveForwardingAddress(ChainADomainID, destRecipient)
	s.Require().NoError(err)

	// Fund with TIA (supported) and an unsupported IBC denom
	tiaAmount := math.NewInt(1000)
	unsupportedDenom := "ibc/ABC123UNSUPPORTED"
	unsupportedAmount := math.NewInt(500)

	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(params.BondDenom, tiaAmount))
	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(unsupportedDenom, unsupportedAmount))

	// Execute forwarding - tx should SUCCEED (partial failure, not full failure)
	msg := forwardingtypes.NewMsgForward(
		s.celestia.SenderAccount.GetAddress().String(),
		sdk.AccAddress(forwardAddr).String(),
		ChainADomainID,
		recipientToHex(destRecipient).String(),
		sdk.NewCoin("utia", math.NewInt(0)), // IGP fee (0 for noop ISM)
	)

	res, err := s.celestia.SendMsgs(msg)
	s.Require().NoError(err, "transaction should succeed even with partial failure")
	s.Require().NotNil(res)

	// Verify: TIA should be drained, unsupported should remain
	newTiaBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	newUnsupportedBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, unsupportedDenom)

	s.True(newTiaBalance.Amount.IsZero(), "TIA should be forwarded")
	s.Equal(unsupportedAmount.Int64(), newUnsupportedBalance.Amount.Int64(), "unsupported token should remain at forwardAddr")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForward_PartialFailure_NoRoute() {
	const (
		CelestiaDomainID uint32 = 69420
		ChainADomainID   uint32 = 1337
		OtherDomainID    uint32 = 9999
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)

	// Set up hyperlane infrastructure on Celestia
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)
	tiaCollatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)

	// Create test token with route to OtherDomainID (NOT ChainADomainID)
	testDenom := "uother"
	testCollatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, testDenom)

	// Set up simapp counterparty for TIA
	ismIDSimapp := s.SetupNoopISM(s.chainA)
	_ = s.SetupMailBox(s.chainA, ismIDSimapp, ChainADomainID)
	tiaSynTokenID := s.CreateSyntheticToken(s.chainA, ismIDSimapp, mailboxIDCelestia)

	// Enroll TIA route to simapp
	s.EnrollRemoteRouter(s.celestia, tiaCollatTokenID, ChainADomainID, tiaSynTokenID.String())
	s.EnrollRemoteRouter(s.chainA, tiaSynTokenID, CelestiaDomainID, tiaCollatTokenID.String())

	// Enroll test token route to OTHER domain only (NOT simapp)
	s.EnrollRemoteRouter(s.celestia, testCollatTokenID, OtherDomainID, "0x0000000000000000000000000000000000000000000000000000000000000001")

	// Derive forwarding address FOR ChainADomainID
	destRecipient := makeRecipient32(s.chainA.SenderAccount.GetAddress())
	forwardAddr, err := forwardingtypes.DeriveForwardingAddress(ChainADomainID, destRecipient)
	s.Require().NoError(err)

	// Fund with both tokens
	tiaAmount := math.NewInt(1000)
	testAmount := math.NewInt(500)
	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(params.BondDenom, tiaAmount))
	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(testDenom, testAmount))

	// Execute forwarding
	msg := forwardingtypes.NewMsgForward(
		s.celestia.SenderAccount.GetAddress().String(),
		sdk.AccAddress(forwardAddr).String(),
		ChainADomainID,
		recipientToHex(destRecipient).String(),
		sdk.NewCoin("utia", math.NewInt(0)), // IGP fee (0 for noop ISM)
	)

	res, err := s.celestia.SendMsgs(msg)
	s.Require().NoError(err, "transaction should succeed with partial failure")
	s.Require().NotNil(res)

	// Verify: TIA forwarded, test token remains (no route to ChainADomainID)
	newTiaBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	newTestBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, testDenom)

	s.True(newTiaBalance.Amount.IsZero(), "TIA should be forwarded (has route to simapp)")
	s.Equal(testAmount.Int64(), newTestBalance.Amount.Int64(), "test token should remain (no route to simapp)")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForward_MinThreshold() {
	const (
		CelestiaDomainID uint32 = 69420
		ChainADomainID   uint32 = 1337
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)

	// Set up hyperlane infrastructure
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)
	tiaCollatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)

	// Set up simapp counterparty
	ismIDSimapp := s.SetupNoopISM(s.chainA)
	_ = s.SetupMailBox(s.chainA, ismIDSimapp, ChainADomainID)
	tiaSynTokenID := s.CreateSyntheticToken(s.chainA, ismIDSimapp, mailboxIDCelestia)

	// Enroll routers
	s.EnrollRemoteRouter(s.celestia, tiaCollatTokenID, ChainADomainID, tiaSynTokenID.String())
	s.EnrollRemoteRouter(s.chainA, tiaSynTokenID, CelestiaDomainID, tiaCollatTokenID.String())

	// Configure params with MINIMUM THRESHOLD of 500
	minThreshold := math.NewInt(500)
	err := celestiaApp.ForwardingKeeper.SetParams(s.celestia.GetContext(), forwardingtypes.NewParams(minThreshold))
	s.Require().NoError(err)

	// Create destination recipient and derive forwarding address
	destRecipient := makeRecipient32(s.chainA.SenderAccount.GetAddress())
	forwardAddr, err := forwardingtypes.DeriveForwardingAddress(ChainADomainID, destRecipient)
	s.Require().NoError(err)

	// Fund with amount BELOW threshold
	belowThresholdAmount := math.NewInt(100)
	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(params.BondDenom, belowThresholdAmount))

	// Execute forwarding - tx FAILS because all tokens fail (below threshold)
	msg := forwardingtypes.NewMsgForward(
		s.celestia.SenderAccount.GetAddress().String(),
		sdk.AccAddress(forwardAddr).String(),
		ChainADomainID,
		recipientToHex(destRecipient).String(),
		sdk.NewCoin("utia", math.NewInt(0)), // IGP fee (0 for noop ISM)
	)

	_, err = s.celestia.SendMsgs(msg)
	s.Require().Error(err, "should fail when all tokens below threshold")
	s.Contains(err.Error(), "all tokens failed")

	// Verify: Token should REMAIN at forwardAddr (below threshold, tx failed)
	newBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	s.Equal(belowThresholdAmount.Int64(), newBalance.Amount.Int64(), "balance should remain unchanged (below threshold)")

	// Add more funds to exceed threshold and verify it forwards
	additionalFunds := math.NewInt(500) // Total will be 600, above 500 threshold
	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(params.BondDenom, additionalFunds))

	// Execute again - should now forward
	res, err := s.celestia.SendMsgs(msg)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	// Verify tokens forwarded
	finalBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	s.True(finalBalance.Amount.IsZero(), "balance should be zero after forwarding (above threshold)")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForward_FullE2E_SourceCollateralToken() {
	const (
		ChainADomainID   uint32 = 1111
		CelestiaDomainID uint32 = 69420
		ChainBDomainID   uint32 = 2222
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)

	// Setup Hyperlane infrastructure on all 3 chains
	ismIDChainA := s.SetupNoopISM(s.chainA)
	mailboxIDChainA := s.SetupMailBox(s.chainA, ismIDChainA, ChainADomainID)
	chainACollatTokenID := s.CreateCollateralToken(s.chainA, ismIDChainA, mailboxIDChainA, sdk.DefaultBondDenom)

	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)
	celestiaSynTokenID := s.CreateSyntheticToken(s.celestia, ismIDCelestia, mailboxIDChainA)

	ismIDChainB := s.SetupNoopISM(s.chainB)
	mailboxIDChainB := s.SetupMailBox(s.chainB, ismIDChainB, ChainBDomainID)
	chainBSynTokenID := s.CreateSyntheticToken(s.chainB, ismIDChainB, mailboxIDChainA)

	// Enroll warp routes
	s.EnrollRemoteRouter(s.chainA, chainACollatTokenID, CelestiaDomainID, celestiaSynTokenID.String())
	s.EnrollRemoteRouter(s.celestia, celestiaSynTokenID, ChainADomainID, chainACollatTokenID.String())
	s.EnrollRemoteRouter(s.celestia, celestiaSynTokenID, ChainBDomainID, chainBSynTokenID.String())
	s.EnrollRemoteRouter(s.chainB, chainBSynTokenID, CelestiaDomainID, celestiaSynTokenID.String())

	// Compute forward address on Celestia
	destRecipient := makeRecipient32(s.chainB.SenderAccount.GetAddress())
	forwardAddr, err := forwardingtypes.DeriveForwardingAddress(ChainBDomainID, destRecipient)
	s.Require().NoError(err)

	// Warp transfer from ChainA to forwardAddr on Celestia
	forwardAddrBytes := makeRecipient32(forwardAddr)
	s.processWarpMessage(s.chainA, s.celestia, mailboxIDCelestia, &warptypes.MsgRemoteTransfer{
		Sender:            s.chainA.SenderAccount.GetAddress().String(),
		TokenId:           chainACollatTokenID,
		DestinationDomain: CelestiaDomainID,
		Recipient:         util.HexAddress(forwardAddrBytes),
		Amount:            math.NewInt(1000),
	})

	// Verify synthetic tokens arrived at forwardAddr
	synToken, err := celestiaApp.WarpKeeper.HypTokens.Get(s.celestia.GetContext(), celestiaSynTokenID.GetInternalId())
	s.Require().NoError(err)
	syntheticDenom := synToken.OriginDenom

	forwardBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, syntheticDenom)
	s.Equal(int64(1000), forwardBalance.Amount.Int64(), "synthetic tokens should arrive at forwardAddr")

	// Execute forwarding on Celestia
	forwardMsg := forwardingtypes.NewMsgForward(
		s.celestia.SenderAccount.GetAddress().String(),
		sdk.AccAddress(forwardAddr).String(),
		ChainBDomainID,
		recipientToHex(destRecipient).String(),
		sdk.NewCoin("utia", math.NewInt(0)), // IGP fee (0 for noop ISM)
	)

	res, err := s.celestia.SendMsgs(forwardMsg)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	hypMsgToChainB := s.extractDispatchMessage(res.Events)
	s.Require().NotEmpty(hypMsgToChainB, "should have hyperlane dispatch message to chainB")

	// Verify forward address is now empty
	newForwardBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, syntheticDenom)
	s.True(newForwardBalance.Amount.IsZero(), "forwardAddr should be empty after forwarding")

	// Process warp message on ChainB
	_, err = s.chainB.SendMsgs(&coretypes.MsgProcessMessage{
		MailboxId: mailboxIDChainB,
		Relayer:   s.chainB.SenderAccount.GetAddress().String(),
		Message:   hypMsgToChainB,
	})
	s.Require().NoError(err)

	// Verify tokens arrived at final destination on ChainB
	chainBApp := s.GetSimapp(s.chainB)
	chainBSynToken, err := chainBApp.WarpKeeper.HypTokens.Get(s.chainB.GetContext(), chainBSynTokenID.GetInternalId())
	s.Require().NoError(err)

	finalBalance := chainBApp.BankKeeper.GetBalance(s.chainB.GetContext(), s.chainB.SenderAccount.GetAddress(), chainBSynToken.OriginDenom)
	s.Equal(int64(1000), finalBalance.Amount.Int64(), "tokens should arrive at final destination on chainB")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForward_FullE2E_TIASyntheticOnSource() {
	const (
		ChainADomainID   uint32 = 1111
		CelestiaDomainID uint32 = 69420
		ChainBDomainID   uint32 = 2222
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)

	// Setup Hyperlane infrastructure
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)
	tiaCollatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)

	ismIDChainA := s.SetupNoopISM(s.chainA)
	mailboxIDChainA := s.SetupMailBox(s.chainA, ismIDChainA, ChainADomainID)
	chainATIASynTokenID := s.CreateSyntheticToken(s.chainA, ismIDChainA, mailboxIDCelestia)

	ismIDChainB := s.SetupNoopISM(s.chainB)
	mailboxIDChainB := s.SetupMailBox(s.chainB, ismIDChainB, ChainBDomainID)
	chainBTIASynTokenID := s.CreateSyntheticToken(s.chainB, ismIDChainB, mailboxIDCelestia)

	// Enroll warp routes
	s.EnrollRemoteRouter(s.celestia, tiaCollatTokenID, ChainADomainID, chainATIASynTokenID.String())
	s.EnrollRemoteRouter(s.chainA, chainATIASynTokenID, CelestiaDomainID, tiaCollatTokenID.String())
	s.EnrollRemoteRouter(s.celestia, tiaCollatTokenID, ChainBDomainID, chainBTIASynTokenID.String())
	s.EnrollRemoteRouter(s.chainB, chainBTIASynTokenID, CelestiaDomainID, tiaCollatTokenID.String())

	// Bridge TIA from Celestia to ChainA to create synthetic TIA
	chainARecipient := makeRecipient32(s.chainA.SenderAccount.GetAddress())
	s.processWarpMessage(s.celestia, s.chainA, mailboxIDChainA, &warptypes.MsgRemoteTransfer{
		Sender:            s.celestia.SenderAccount.GetAddress().String(),
		TokenId:           tiaCollatTokenID,
		DestinationDomain: ChainADomainID,
		Recipient:         util.HexAddress(chainARecipient),
		Amount:            math.NewInt(2000),
	})

	// Verify synthetic TIA arrived on chainA
	chainAApp := s.GetSimapp(s.chainA)
	chainATIAToken, err := chainAApp.WarpKeeper.HypTokens.Get(s.chainA.GetContext(), chainATIASynTokenID.GetInternalId())
	s.Require().NoError(err)
	chainATIABalance := chainAApp.BankKeeper.GetBalance(s.chainA.GetContext(), s.chainA.SenderAccount.GetAddress(), chainATIAToken.OriginDenom)
	s.Equal(int64(2000), chainATIABalance.Amount.Int64())

	// Compute forward address on Celestia for ChainB
	destRecipient := makeRecipient32(s.chainB.SenderAccount.GetAddress())
	forwardAddr, err := forwardingtypes.DeriveForwardingAddress(ChainBDomainID, destRecipient)
	s.Require().NoError(err)

	// Warp TIA synthetic back to forwardAddr on Celestia (releases collateral)
	forwardAddrBytes := makeRecipient32(forwardAddr)
	s.processWarpMessage(s.chainA, s.celestia, mailboxIDCelestia, &warptypes.MsgRemoteTransfer{
		Sender:            s.chainA.SenderAccount.GetAddress().String(),
		TokenId:           chainATIASynTokenID,
		DestinationDomain: CelestiaDomainID,
		Recipient:         util.HexAddress(forwardAddrBytes),
		Amount:            math.NewInt(1000),
	})

	// Verify NATIVE utia arrived at forwardAddr
	forwardBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	s.Equal(int64(1000), forwardBalance.Amount.Int64(), "native utia should arrive at forwardAddr")

	// Execute forwarding on Celestia
	forwardMsg := forwardingtypes.NewMsgForward(
		s.celestia.SenderAccount.GetAddress().String(),
		sdk.AccAddress(forwardAddr).String(),
		ChainBDomainID,
		recipientToHex(destRecipient).String(),
		sdk.NewCoin("utia", math.NewInt(0)), // IGP fee (0 for noop ISM)
	)

	res, err := s.celestia.SendMsgs(forwardMsg)
	s.Require().NoError(err)

	hypMsgToChainB := s.extractDispatchMessage(res.Events)

	// Verify forward address is now empty
	newForwardBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	s.True(newForwardBalance.Amount.IsZero(), "forwardAddr should be empty after forwarding")

	// Process warp message on ChainB
	_, err = s.chainB.SendMsgs(&coretypes.MsgProcessMessage{
		MailboxId: mailboxIDChainB,
		Relayer:   s.chainB.SenderAccount.GetAddress().String(),
		Message:   hypMsgToChainB,
	})
	s.Require().NoError(err)

	// Verify synthetic TIA arrived at final destination on ChainB
	chainBApp := s.GetSimapp(s.chainB)
	chainBTIAToken, err := chainBApp.WarpKeeper.HypTokens.Get(s.chainB.GetContext(), chainBTIASynTokenID.GetInternalId())
	s.Require().NoError(err)

	finalBalance := chainBApp.BankKeeper.GetBalance(s.chainB.GetContext(), s.chainB.SenderAccount.GetAddress(), chainBTIAToken.OriginDenom)
	s.Equal(int64(1000), finalBalance.Amount.Int64(), "synthetic TIA should arrive at final destination")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForward_FullE2E_CEXWithdrawal() {
	const (
		CelestiaDomainID uint32 = 69420
		ChainBDomainID   uint32 = 2222
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)

	// Setup Hyperlane infrastructure
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)
	tiaCollatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)

	ismIDChainB := s.SetupNoopISM(s.chainB)
	mailboxIDChainB := s.SetupMailBox(s.chainB, ismIDChainB, ChainBDomainID)
	chainBTIASynTokenID := s.CreateSyntheticToken(s.chainB, ismIDChainB, mailboxIDCelestia)

	// Enroll warp routes
	s.EnrollRemoteRouter(s.celestia, tiaCollatTokenID, ChainBDomainID, chainBTIASynTokenID.String())
	s.EnrollRemoteRouter(s.chainB, chainBTIASynTokenID, CelestiaDomainID, tiaCollatTokenID.String())

	// Compute forward address on Celestia
	destRecipient := makeRecipient32(s.chainB.SenderAccount.GetAddress())
	forwardAddr, err := forwardingtypes.DeriveForwardingAddress(ChainBDomainID, destRecipient)
	s.Require().NoError(err)

	// Simulate CEX withdrawal by directly funding the forward address
	cexWithdrawalAmount := math.NewInt(5000)
	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(params.BondDenom, cexWithdrawalAmount))

	// Verify funds arrived
	forwardBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	s.Equal(cexWithdrawalAmount.Int64(), forwardBalance.Amount.Int64())

	// Execute forwarding on Celestia
	forwardMsg := forwardingtypes.NewMsgForward(
		s.celestia.SenderAccount.GetAddress().String(),
		sdk.AccAddress(forwardAddr).String(),
		ChainBDomainID,
		recipientToHex(destRecipient).String(),
		sdk.NewCoin("utia", math.NewInt(0)), // IGP fee (0 for noop ISM)
	)

	res, err := s.celestia.SendMsgs(forwardMsg)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	hypMsgToChainB := s.extractDispatchMessage(res.Events)
	s.Require().NotEmpty(hypMsgToChainB)

	// Verify forward address is now empty
	newForwardBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	s.True(newForwardBalance.Amount.IsZero(), "forwardAddr should be empty after forwarding")

	// Process warp message on ChainB
	_, err = s.chainB.SendMsgs(&coretypes.MsgProcessMessage{
		MailboxId: mailboxIDChainB,
		Relayer:   s.chainB.SenderAccount.GetAddress().String(),
		Message:   hypMsgToChainB,
	})
	s.Require().NoError(err)

	// Verify synthetic TIA arrived at final destination on ChainB
	chainBApp := s.GetSimapp(s.chainB)
	chainBTIAToken, err := chainBApp.WarpKeeper.HypTokens.Get(s.chainB.GetContext(), chainBTIASynTokenID.GetInternalId())
	s.Require().NoError(err)

	finalBalance := chainBApp.BankKeeper.GetBalance(s.chainB.GetContext(), s.chainB.SenderAccount.GetAddress(), chainBTIAToken.OriginDenom)
	s.Equal(cexWithdrawalAmount.Int64(), finalBalance.Amount.Int64(), "synthetic TIA should arrive at final destination")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForward_TooManyTokens() {
	const (
		CelestiaDomainID uint32 = 69420
		ChainADomainID   uint32 = 1337
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Setup hyperlane infrastructure
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)
	tiaCollatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)

	ismIDSimapp := s.SetupNoopISM(s.chainA)
	_ = s.SetupMailBox(s.chainA, ismIDSimapp, ChainADomainID)
	tiaSynTokenID := s.CreateSyntheticToken(s.chainA, ismIDSimapp, mailboxIDCelestia)

	// Enroll routers for TIA
	s.EnrollRemoteRouter(s.celestia, tiaCollatTokenID, ChainADomainID, tiaSynTokenID.String())
	s.EnrollRemoteRouter(s.chainA, tiaSynTokenID, CelestiaDomainID, tiaCollatTokenID.String())

	// Derive forwarding address
	destRecipient := makeRecipient32(s.chainA.SenderAccount.GetAddress())
	forwardAddr, err := forwardingtypes.DeriveForwardingAddress(ChainADomainID, destRecipient)
	s.Require().NoError(err)

	// Fund with utia (has route) - will be first in sort order since "utia" < "ztoken"
	tiaAmount := math.NewInt(1000)
	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(params.BondDenom, tiaAmount))

	// Create 20 ztoken denoms (sort after utia) = 21 total tokens
	// Using "ztoken" because it sorts AFTER "utia" alphabetically,
	// ensuring utia is included in the first 20 processed tokens
	for i := range forwardingtypes.MaxTokensPerForward {
		denom := fmt.Sprintf("ztoken%02d", i)
		coin := sdk.NewCoin(denom, math.NewInt(100))
		err := celestiaApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, sdk.NewCoins(coin))
		s.Require().NoError(err)
		err = celestiaApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, forwardAddr, sdk.NewCoins(coin))
		s.Require().NoError(err)
	}

	// Verify we have 21 different tokens (1 utia + 20 ztokens)
	balances := celestiaApp.BankKeeper.GetAllBalances(ctx, forwardAddr)
	s.Equal(forwardingtypes.MaxTokensPerForward+1, len(balances), "should have 21 different tokens")

	// Forward - processes first 20: utia + ztoken00..ztoken18
	// utia succeeds (has route), ztokens fail (no routes)
	forwardMsg := forwardingtypes.NewMsgForward(
		s.celestia.SenderAccount.GetAddress().String(),
		sdk.AccAddress(forwardAddr).String(),
		ChainADomainID,
		recipientToHex(destRecipient).String(),
		sdk.NewCoin("utia", math.NewInt(0)), // IGP fee (0 for noop ISM)
	)

	res, err := s.celestia.SendMsgs(forwardMsg)
	s.Require().NoError(err, "partial success - utia forwards, ztokens fail")
	s.Require().NotNil(res)

	// Verify dispatch event (utia forwarded)
	s.Equal(1, s.countDispatchEvents(res.Events), "utia should dispatch")

	// Verify utia was forwarded (balance = 0)
	newTiaBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	s.True(newTiaBalance.Amount.IsZero(), "utia should be forwarded")

	// Verify ztokens remain:
	// - 19 ztokens processed but failed (no routes)
	// - 1 ztoken (ztoken19) not processed due to truncation
	// Total: 20 ztokens remain
	remainingBalances := celestiaApp.BankKeeper.GetAllBalances(s.celestia.GetContext(), forwardAddr)
	s.Equal(20, len(remainingBalances), "20 ztokens should remain (no utia)")
}
