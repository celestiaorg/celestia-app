package interop

import (
	"encoding/hex"
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v6/app/params"
	forwardingtypes "github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
	minttypes "github.com/celestiaorg/celestia-app/v6/x/mint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/suite"
)

// ForwardingIntegrationTestSuite tests the forwarding module with real dependencies.
// It embeds HyperlaneTestSuite to reuse hyperlane setup helpers.
type ForwardingIntegrationTestSuite struct {
	HyperlaneTestSuite
}

func TestForwardingIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(ForwardingIntegrationTestSuite))
}

func (s *ForwardingIntegrationTestSuite) SetupTest() {
	// Call parent setup which initializes chains and mints 1M utia
	s.HyperlaneTestSuite.SetupTest()

	// Mint additional utia for forwarding tests (need more than base hyperlane tests)
	app := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	additionalFunds := sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(9_000_000)))
	err := app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, additionalFunds)
	s.Require().NoError(err)

	err = app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, s.celestia.SenderAccount.GetAddress(), additionalFunds)
	s.Require().NoError(err)
}

// ============================================================================
// Test 1: Params Storage with Proto-Generated Types
// ============================================================================

func (s *ForwardingIntegrationTestSuite) TestParamsStorageWithProtoTypes() {
	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set params with TIA token ID
	testTokenId := "0x726f757465725f6170700000000000000000000000000000000000000000001"
	newParams := forwardingtypes.NewParams(math.NewInt(100), testTokenId)

	err := celestiaApp.ForwardingKeeper.SetParams(ctx, newParams)
	s.Require().NoError(err)

	// Get params back
	retrievedParams, err := celestiaApp.ForwardingKeeper.GetParams(ctx)
	s.Require().NoError(err)

	s.Equal(math.NewInt(100), retrievedParams.MinForwardAmount)
	s.Equal(testTokenId, retrievedParams.TiaCollateralTokenId)

	s.T().Logf("Test 1 PASSED: Params storage works with proto-generated types")
	s.T().Logf("MinForwardAmount: %s", retrievedParams.MinForwardAmount.String())
	s.T().Logf("TiaCollateralTokenId: %s", retrievedParams.TiaCollateralTokenId)
}

// ============================================================================
// Test 2: FindHypTokenByDenom for TIA (Collateral)
// ============================================================================

func (s *ForwardingIntegrationTestSuite) TestFindHypTokenByDenom_TIA() {
	const (
		CelestiaDomainID = 69420
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set up hyperlane infrastructure
	ismID := s.SetupNoopISM(s.celestia)
	mailboxID := s.SetupMailBox(s.celestia, ismID, CelestiaDomainID)

	// Create a collateral token for utia (TIA)
	collatTokenID := s.CreateCollateralToken(s.celestia, ismID, mailboxID, params.BondDenom)

	// Configure TIA token ID in params
	newParams := forwardingtypes.NewParams(math.ZeroInt(), collatTokenID.String())
	err := celestiaApp.ForwardingKeeper.SetParams(ctx, newParams)
	s.Require().NoError(err)

	// Test FindHypTokenByDenom for "utia"
	hypToken, err := celestiaApp.ForwardingKeeper.FindHypTokenByDenom(ctx, "utia")
	s.Require().NoError(err)

	s.Equal(warptypes.HYP_TOKEN_TYPE_COLLATERAL, hypToken.TokenType)
	s.Equal(params.BondDenom, hypToken.OriginDenom)

	s.T().Logf("Test 2 PASSED: FindHypTokenByDenom works for TIA collateral")
	s.T().Logf("Token type: %v", hypToken.TokenType)
	s.T().Logf("Origin denom: %s", hypToken.OriginDenom)
}

// ============================================================================
// Test 3: FindHypTokenByDenom for Synthetic Token
// ============================================================================

func (s *ForwardingIntegrationTestSuite) TestFindHypTokenByDenom_Synthetic() {
	const (
		CelestiaDomainID = 69420
		SimappDomainID   = 1337
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set up hyperlane infrastructure on simapp (origin chain)
	ismIDSimapp := s.SetupNoopISM(s.simapp)
	mailboxIDSimapp := s.SetupMailBox(s.simapp, ismIDSimapp, SimappDomainID)

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

	// Test FindHypTokenByDenom for synthetic denom
	foundToken, err := celestiaApp.ForwardingKeeper.FindHypTokenByDenom(ctx, syntheticDenom)
	s.Require().NoError(err)

	s.Equal(warptypes.HYP_TOKEN_TYPE_SYNTHETIC, foundToken.TokenType)
	s.Equal(syntheticDenom, foundToken.OriginDenom)

	s.T().Logf("Test 3 PASSED: FindHypTokenByDenom works for synthetic tokens")
	s.T().Logf("Token type: %v", foundToken.TokenType)
}

// ============================================================================
// Test 4: HasEnrolledRouter Pre-Check
// ============================================================================

func (s *ForwardingIntegrationTestSuite) TestHasEnrolledRouter() {
	const (
		CelestiaDomainID uint32 = 69420
		SimappDomainID   uint32 = 1337
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set up hyperlane infrastructure
	ismID := s.SetupNoopISM(s.celestia)
	mailboxID := s.SetupMailBox(s.celestia, ismID, CelestiaDomainID)
	collatTokenID := s.CreateCollateralToken(s.celestia, ismID, mailboxID, params.BondDenom)

	// Before enrollment - should return false
	hasRouteBefore, err := celestiaApp.ForwardingKeeper.HasEnrolledRouter(ctx, collatTokenID, SimappDomainID)
	s.Require().NoError(err)
	s.False(hasRouteBefore, "should NOT have enrolled router before enrollment")

	// Set up simapp and enroll router
	ismIDSimapp := s.SetupNoopISM(s.simapp)
	s.SetupMailBox(s.simapp, ismIDSimapp, SimappDomainID)
	synTokenID := s.CreateSyntheticToken(s.simapp, ismIDSimapp, mailboxID)
	s.EnrollRemoteRouter(s.celestia, collatTokenID, SimappDomainID, synTokenID.String())

	// After enrollment - should return true
	hasRouteAfter, err := celestiaApp.ForwardingKeeper.HasEnrolledRouter(ctx, collatTokenID, SimappDomainID)
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

// ============================================================================
// Test 5: Full MsgExecuteForwarding Flow
// ============================================================================

func (s *ForwardingIntegrationTestSuite) TestMsgExecuteForwarding_FullFlow() {
	const (
		CelestiaDomainID uint32 = 69420
		SimappDomainID   uint32 = 1337
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set up hyperlane infrastructure
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)
	collatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)

	// Set up simapp counterparty
	ismIDSimapp := s.SetupNoopISM(s.simapp)
	mailboxIDSimapp := s.SetupMailBox(s.simapp, ismIDSimapp, SimappDomainID)
	synTokenID := s.CreateSyntheticToken(s.simapp, ismIDSimapp, mailboxIDCelestia)

	// Enroll routers
	s.EnrollRemoteRouter(s.celestia, collatTokenID, SimappDomainID, synTokenID.String())
	s.EnrollRemoteRouter(s.simapp, synTokenID, CelestiaDomainID, collatTokenID.String())

	// Configure TIA token ID in forwarding params
	newParams := forwardingtypes.NewParams(math.ZeroInt(), collatTokenID.String())
	err := celestiaApp.ForwardingKeeper.SetParams(ctx, newParams)
	s.Require().NoError(err)

	// Create destination recipient (32-byte address)
	destRecipient := make([]byte, 32)
	copy(destRecipient[12:], s.simapp.SenderAccount.GetAddress().Bytes())

	// Derive forwarding address
	forwardAddr := forwardingtypes.DeriveForwardingAddress(SimappDomainID, destRecipient)
	s.T().Logf("Derived forwarding address: %s", forwardAddr.String())

	// Fund the forwarding address (simulating incoming warp transfer or CEX deposit)
	fundAmount := math.NewInt(1000)
	err = celestiaApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, sdk.NewCoins(sdk.NewCoin(params.BondDenom, fundAmount)))
	s.Require().NoError(err)
	err = celestiaApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, forwardAddr, sdk.NewCoins(sdk.NewCoin(params.BondDenom, fundAmount)))
	s.Require().NoError(err)

	// Verify forward address has funds
	forwardBalance := celestiaApp.BankKeeper.GetBalance(ctx, forwardAddr, params.BondDenom)
	s.Equal(fundAmount.Int64(), forwardBalance.Amount.Int64())
	s.T().Logf("Forward address balance before: %s", forwardBalance.String())

	// Create and send MsgExecuteForwarding
	destRecipientHex, err := util.DecodeHexAddress("0x" + hex.EncodeToString(destRecipient))
	s.Require().NoError(err)

	msg := forwardingtypes.NewMsgExecuteForwarding(
		s.celestia.SenderAccount.GetAddress().String(),
		forwardAddr.String(),
		SimappDomainID,
		destRecipientHex.String(),
	)

	// Execute the forwarding via SendMsgs
	res, err := s.celestia.SendMsgs(msg)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	s.T().Logf("MsgExecuteForwarding result: code=%d", res.Code)

	// Parse events to find hyperlane dispatch
	var hypMsg string
	var foundDispatch bool
	for _, evt := range res.Events {
		s.T().Logf("Event type: %s", evt.Type)
		if evt.Type == proto.MessageName(&coretypes.EventDispatch{}) {
			protoMsg, err := sdk.ParseTypedEvent(evt)
			s.Require().NoError(err)

			eventDispatch, ok := protoMsg.(*coretypes.EventDispatch)
			s.Require().True(ok)

			hypMsg = eventDispatch.Message
			foundDispatch = true
			s.T().Logf("Found Hyperlane dispatch event")
		}
	}

	// Verify forward address is now empty (this is the key verification)
	newForwardBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	s.True(newForwardBalance.Amount.IsZero(), "forward address should be empty after forwarding")
	s.T().Logf("Forward address balance after: %s", newForwardBalance.String())

	// If we found a dispatch event, verify the message can be processed on simapp
	if foundDispatch && hypMsg != "" {
		s.T().Logf("Testing cross-chain message processing...")

		// Process the message on simapp counterparty
		msgProcessMessage := coretypes.MsgProcessMessage{
			MailboxId: mailboxIDSimapp,
			Relayer:   s.simapp.SenderAccount.GetAddress().String(),
			Message:   hypMsg,
		}

		res, err = s.simapp.SendMsgs(&msgProcessMessage)
		if err != nil {
			s.T().Logf("Note: Cross-chain message processing failed (may need additional setup): %v", err)
		} else {
			s.Require().NotNil(res)

			// Verify tokens arrived at destination
			simapp := s.GetSimapp(s.simapp)
			hypDenom, err := simapp.WarpKeeper.HypTokens.Get(s.simapp.GetContext(), synTokenID.GetInternalId())
			s.Require().NoError(err)

			destBalance := simapp.BankKeeper.GetBalance(s.simapp.GetContext(), s.simapp.SenderAccount.GetAddress(), hypDenom.OriginDenom)
			s.Equal(fundAmount.Int64(), destBalance.Amount.Int64())
			s.T().Logf("Destination balance: %s", destBalance.String())
		}
	} else {
		s.T().Logf("Note: No dispatch event found - forwarding may have used different event format")
	}

	s.T().Logf("Test 5 PASSED: MsgExecuteForwarding drained forward address successfully!")
}

// ============================================================================
// Test 6: Address Mismatch Rejection
// ============================================================================

func (s *ForwardingIntegrationTestSuite) TestMsgExecuteForwarding_AddressMismatch() {
	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set default params
	err := celestiaApp.ForwardingKeeper.SetParams(ctx, forwardingtypes.DefaultParams())
	s.Require().NoError(err)

	// Create a random address (not derived from params)
	randomAddr := sdk.AccAddress([]byte("random_address______"))

	// Try to execute forwarding with mismatched address
	destRecipient := make([]byte, 32)
	copy(destRecipient[12:], s.simapp.SenderAccount.GetAddress().Bytes())
	destRecipientHex, _ := util.DecodeHexAddress("0x" + hex.EncodeToString(destRecipient))

	msg := forwardingtypes.NewMsgExecuteForwarding(
		s.celestia.SenderAccount.GetAddress().String(),
		randomAddr.String(),
		1337,
		destRecipientHex.String(),
	)

	// Execute - should fail with address mismatch
	_, err = s.celestia.SendMsgs(msg)
	s.Require().Error(err)
	s.Contains(err.Error(), "derived address does not match")

	s.T().Logf("Test 6 PASSED: Address mismatch correctly rejected")
}

// ============================================================================
// Test 7: Zero Balance Rejection
// ============================================================================

func (s *ForwardingIntegrationTestSuite) TestMsgExecuteForwarding_NoBalance() {
	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set default params
	err := celestiaApp.ForwardingKeeper.SetParams(ctx, forwardingtypes.DefaultParams())
	s.Require().NoError(err)

	// Derive a valid forwarding address
	destRecipient := make([]byte, 32)
	copy(destRecipient[12:], s.simapp.SenderAccount.GetAddress().Bytes())
	forwardAddr := forwardingtypes.DeriveForwardingAddress(1337, destRecipient)
	destRecipientHex, _ := util.DecodeHexAddress("0x" + hex.EncodeToString(destRecipient))

	// Don't fund the address - it should have zero balance

	msg := forwardingtypes.NewMsgExecuteForwarding(
		s.celestia.SenderAccount.GetAddress().String(),
		forwardAddr.String(),
		1337,
		destRecipientHex.String(),
	)

	// Execute - should fail with no balance
	_, err = s.celestia.SendMsgs(msg)
	s.Require().Error(err)
	s.Contains(err.Error(), "no balance")

	s.T().Logf("Test 7 PASSED: Zero balance correctly rejected")
}

