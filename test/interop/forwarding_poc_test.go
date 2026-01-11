package interop

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/params"
	minttypes "github.com/celestiaorg/celestia-app/v6/x/mint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/suite"
)

// ForwardingPoCTestSuite contains proof-of-concept tests for the forwarding module.
// These tests verify core assumptions before implementing the full module.
type ForwardingPoCTestSuite struct {
	suite.Suite

	celestia *ibctesting.TestChain
	simapp   *ibctesting.TestChain
}

func TestForwardingPoCTestSuite(t *testing.T) {
	suite.Run(t, new(ForwardingPoCTestSuite))
}

func (s *ForwardingPoCTestSuite) SetupTest() {
	_, celestia, simapp, _ := SetupTest(s.T())

	s.celestia = celestia
	s.simapp = simapp

	// Fund the sender account with utia (test infra uses "stake" by default)
	app := s.GetCelestiaApp(celestia)
	err := app.BankKeeper.MintCoins(celestia.GetContext(), minttypes.ModuleName, sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(10_000_000))))
	s.Require().NoError(err)

	err = app.BankKeeper.SendCoinsFromModuleToAccount(celestia.GetContext(), minttypes.ModuleName, celestia.SenderAccount.GetAddress(), sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(10_000_000))))
	s.Require().NoError(err)
}

func (s *ForwardingPoCTestSuite) GetCelestiaApp(chain *ibctesting.TestChain) *app.App {
	app, ok := chain.App.(*app.App)
	s.Require().True(ok)
	return app
}

// DeriveForwardingAddress derives a forwarding address using the planned algorithm.
// This matches the implementation_spec.md address derivation.
func DeriveForwardingAddress(destDomain uint32, destRecipient []byte) sdk.AccAddress {
	// Compute call digest: keccak256(abi.encode(destDomain, destRecipient))
	// destDomain is padded to 32 bytes (big-endian at bytes [28:32])
	destDomainBytes := make([]byte, 32)
	binary.BigEndian.PutUint32(destDomainBytes[28:], destDomain)

	// Concatenate destDomain (32 bytes) + destRecipient (32 bytes)
	callDigestPreimage := append(destDomainBytes, destRecipient...)
	callDigest := crypto.Keccak256(callDigestPreimage)

	// Compute salt: keccak256("CELESTIA_FORWARD_V1" || callDigest)
	saltPreimage := append([]byte("CELESTIA_FORWARD_V1"), callDigest...)
	salt := crypto.Keccak256(saltPreimage)

	// Derive address: sha256(moduleName || salt)[:20]
	// Using "forwarding" as the module name
	moduleName := "forwarding"
	addressPreimage := append([]byte(moduleName), salt...)
	hash := sha256.Sum256(addressPreimage)

	return sdk.AccAddress(hash[:20])
}

// ============================================================================
// PoC 1: Test SendCoins from Derived Addresses
// ============================================================================
// CRITICAL TEST: Verifies that the bank module allows SendCoins from addresses
// that the module doesn't "own" in the traditional sense.
//
// This is the most important PoC - if it fails, we need alternative approaches.
// ============================================================================

func (s *ForwardingPoCTestSuite) TestPoC1_SendCoinsFromDerivedAddress() {
	app := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// 1. Derive a forwarding address
	destDomain := uint32(1)
	destRecipient := make([]byte, 32) // Zero-padded recipient
	copy(destRecipient[12:], s.simapp.SenderAccount.GetAddress().Bytes())
	forwardAddr := DeriveForwardingAddress(destDomain, destRecipient)

	s.T().Logf("Derived forwarding address: %s", forwardAddr.String())

	// 2. Fund the derived address directly (simulating incoming warp transfer)
	fundAmount := math.NewInt(1000)
	err := app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, sdk.NewCoins(sdk.NewCoin(params.BondDenom, fundAmount)))
	s.Require().NoError(err)

	err = app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, forwardAddr, sdk.NewCoins(sdk.NewCoin(params.BondDenom, fundAmount)))
	s.Require().NoError(err)

	// Verify the forwarding address has the funds
	balance := app.BankKeeper.GetBalance(ctx, forwardAddr, params.BondDenom)
	s.Require().Equal(fundAmount.Int64(), balance.Amount.Int64(), "forwarding address should have the funds")

	// 3. Try SendCoins FROM forwardAddr TO another address
	// This is the critical test - can we spend from forwardAddr without special permissions?
	recipientAddr := s.celestia.SenderAccount.GetAddress()
	originalRecipientBalance := app.BankKeeper.GetBalance(ctx, recipientAddr, params.BondDenom)

	// Attempt to send coins from the derived address
	// NOTE: This uses the bank keeper directly, which requires special authority.
	// In the actual module, we'll need to see if this works.
	err = app.BankKeeper.SendCoins(ctx, forwardAddr, recipientAddr, sdk.NewCoins(sdk.NewCoin(params.BondDenom, fundAmount)))

	// ============================================================================
	// CRITICAL RESULT CHECK
	// ============================================================================
	if err != nil {
		s.T().Logf("PoC 1 FAILED: SendCoins from derived address returned error: %v", err)
		s.T().Logf("This means we need an alternative approach (e.g., module authority, SendCoinsFromAccountToModule)")
		s.Fail("SendCoins from derived address failed - need alternative approach")
	} else {
		s.T().Logf("PoC 1 PASSED: SendCoins from derived address succeeded!")

		// Verify the transfer
		newForwardBalance := app.BankKeeper.GetBalance(ctx, forwardAddr, params.BondDenom)
		newRecipientBalance := app.BankKeeper.GetBalance(ctx, recipientAddr, params.BondDenom)

		s.True(newForwardBalance.Amount.IsZero(), "forwarding address should be empty")
		expectedRecipientBalance := originalRecipientBalance.Amount.Add(fundAmount)
		s.True(newRecipientBalance.Amount.Equal(expectedRecipientBalance), "recipient should have received funds")

		s.T().Logf("Forward address balance after: %s", newForwardBalance.Amount.String())
		s.T().Logf("Recipient balance after: %s", newRecipientBalance.Amount.String())
	}
}

// ============================================================================
// PoC 2: Test Derived Address Receives Funds
// ============================================================================
// Verifies that a derived address can receive funds without being registered.
// ============================================================================

func (s *ForwardingPoCTestSuite) TestPoC2_DerivedAddressReceivesFunds() {
	app := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// 1. Derive a forwarding address
	destDomain := uint32(2)
	destRecipient := make([]byte, 32)
	copy(destRecipient[12:], s.simapp.SenderAccount.GetAddress().Bytes())
	forwardAddr := DeriveForwardingAddress(destDomain, destRecipient)

	s.T().Logf("Derived forwarding address: %s", forwardAddr.String())

	// 2. Verify it starts with zero balance
	initialBalance := app.BankKeeper.GetBalance(ctx, forwardAddr, params.BondDenom)
	s.Equal(int64(0), initialBalance.Amount.Int64(), "derived address should start with zero balance")

	// 3. Fund the address
	fundAmount := math.NewInt(5000)
	err := app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, sdk.NewCoins(sdk.NewCoin(params.BondDenom, fundAmount)))
	s.Require().NoError(err)

	err = app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, forwardAddr, sdk.NewCoins(sdk.NewCoin(params.BondDenom, fundAmount)))
	s.Require().NoError(err)

	// 4. Verify the balance
	balance := app.BankKeeper.GetBalance(ctx, forwardAddr, params.BondDenom)
	s.Equal(fundAmount.Int64(), balance.Amount.Int64())

	s.T().Logf("PoC 2 PASSED: Derived address received funds successfully")
	s.T().Logf("Balance: %d %s", balance.Amount.Int64(), balance.Denom)
}

// ============================================================================
// PoC 3: TIA Collateral Token ID Investigation
// ============================================================================
// Investigates how TIA collateral token is configured and how to find its ID.
// ============================================================================

func (s *ForwardingPoCTestSuite) TestPoC3_TIACollateralTokenSetup() {
	const (
		CelestiaDomainID = 69420
		SimappDomainID   = 1337
	)

	app := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set up hyperlane infrastructure
	ismID := s.SetupNoopISM(s.celestia)
	mailboxID := s.SetupMailBox(s.celestia, ismID, CelestiaDomainID)

	// Create a collateral token for utia (TIA)
	collatTokenID := s.CreateCollateralToken(s.celestia, ismID, mailboxID, params.BondDenom)

	s.T().Logf("Created TIA collateral token with ID: %s", collatTokenID.String())
	s.T().Logf("Token ID internal: %d", collatTokenID.GetInternalId())

	// Verify we can retrieve the token
	hypToken, err := app.WarpKeeper.HypTokens.Get(ctx, collatTokenID.GetInternalId())
	s.Require().NoError(err)

	s.T().Logf("PoC 3 PASSED: TIA collateral token investigation complete")
	s.T().Logf("Token type: %v", hypToken.TokenType)
	s.T().Logf("Origin denom: %s", hypToken.OriginDenom)
	s.T().Logf("Origin mailbox: %s", hypToken.OriginMailbox.String())

	// Key insight: Token ID must be known (hardcoded or from params) to look up TIA token
	// In production, this would be configured at genesis or via governance
	s.Equal(params.BondDenom, hypToken.OriginDenom, "origin denom should be utia")
	s.Equal(warptypes.HYP_TOKEN_TYPE_COLLATERAL, hypToken.TokenType, "should be collateral type")
}

// ============================================================================
// PoC 4: Warp Route Pre-Check (EnrolledRouters.Has)
// ============================================================================
// Verifies that we can check if a warp route exists before attempting transfer.
// This is needed for the pre-check pattern that keeps failed tokens at forwardAddr.
// ============================================================================

func (s *ForwardingPoCTestSuite) TestPoC4_WarpRoutePreCheck() {
	const (
		CelestiaDomainID uint32 = 69420
		SimappDomainID   uint32 = 1337
	)

	app := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set up hyperlane infrastructure
	ismID := s.SetupNoopISM(s.celestia)
	mailboxID := s.SetupMailBox(s.celestia, ismID, CelestiaDomainID)

	// Create a collateral token for utia
	collatTokenID := s.CreateCollateralToken(s.celestia, ismID, mailboxID, params.BondDenom)

	// BEFORE enrolling router - check if route exists
	hasRouteBeforeEnroll, err := app.WarpKeeper.EnrolledRouters.Has(ctx,
		collections.Join(collatTokenID.GetInternalId(), SimappDomainID))
	s.Require().NoError(err)
	s.False(hasRouteBeforeEnroll, "should NOT have enrolled router before enrollment")

	s.T().Logf("Before enrollment - HasEnrolledRouter: %v", hasRouteBeforeEnroll)

	// Now enroll a router
	// First, set up simapp side
	ismIDSimapp := s.SetupNoopISM(s.simapp)
	_ = s.SetupMailBox(s.simapp, ismIDSimapp, SimappDomainID) // mailbox not used directly
	synTokenID := s.CreateSyntheticToken(s.simapp, ismIDSimapp, mailboxID)

	// Enroll the remote router
	s.EnrollRemoteRouter(s.celestia, collatTokenID, SimappDomainID, synTokenID.String())

	// AFTER enrolling router - check again
	hasRouteAfterEnroll, err := app.WarpKeeper.EnrolledRouters.Has(ctx,
		collections.Join(collatTokenID.GetInternalId(), SimappDomainID))
	s.Require().NoError(err)
	s.True(hasRouteAfterEnroll, "should have enrolled router after enrollment")

	s.T().Logf("After enrollment - HasEnrolledRouter: %v", hasRouteAfterEnroll)

	// Verify we can also get the router details
	router, err := app.WarpKeeper.EnrolledRouters.Get(ctx,
		collections.Join(collatTokenID.GetInternalId(), SimappDomainID))
	s.Require().NoError(err)
	s.Equal(SimappDomainID, router.ReceiverDomain)

	s.T().Logf("PoC 4 PASSED: Warp route pre-check works!")
	s.T().Logf("Router receiver domain: %d", router.ReceiverDomain)
	s.T().Logf("Router receiver contract: %s", router.ReceiverContract)

	// Test for non-existent domain
	var nonExistentDomain uint32 = 99999
	hasNonExistent, err := app.WarpKeeper.EnrolledRouters.Has(ctx,
		collections.Join(collatTokenID.GetInternalId(), nonExistentDomain))
	s.Require().NoError(err)
	s.False(hasNonExistent, "should NOT have router for non-existent domain")

	s.T().Logf("Non-existent domain check: HasEnrolledRouter = %v", hasNonExistent)
}

// ============================================================================
// PoC 5: Synthetic Token Denom Format
// ============================================================================
// Verifies the denom format for synthetic tokens (hyperlane/{tokenId}).
// ============================================================================

func (s *ForwardingPoCTestSuite) TestPoC5_SyntheticTokenDenomFormat() {
	const (
		CelestiaDomainID = 69420
		SimappDomainID   = 1337
	)

	app := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set up hyperlane infrastructure on simapp
	ismIDSimapp := s.SetupNoopISM(s.simapp)
	mailboxIDSimapp := s.SetupMailBox(s.simapp, ismIDSimapp, SimappDomainID)

	// Set up celestia
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)

	// Create a synthetic token on celestia (representing a token from simapp)
	synTokenID := s.CreateSyntheticToken(s.celestia, ismIDCelestia, mailboxIDSimapp)

	// Get the token details
	hypToken, err := app.WarpKeeper.HypTokens.Get(ctx, synTokenID.GetInternalId())
	s.Require().NoError(err)

	s.T().Logf("PoC 5: Synthetic Token Denom Format")
	s.T().Logf("Synthetic token ID: %s", synTokenID.String())
	s.T().Logf("Origin denom: %s", hypToken.OriginDenom)

	// Verify the denom format matches "hyperlane/{tokenId}"
	expectedDenomPrefix := "hyperlane/"
	s.Contains(hypToken.OriginDenom, expectedDenomPrefix, "synthetic denom should start with 'hyperlane/'")

	s.T().Logf("PoC 5 PASSED: Synthetic token denom format is: %s", hypToken.OriginDenom)
}

// ============================================================================
// Helper methods (reusing patterns from hyperlane_test.go)
// ============================================================================

func (s *ForwardingPoCTestSuite) SetupNoopISM(chain *ibctesting.TestChain) util.HexAddress {
	suite := &HyperlaneTestSuite{Suite: s.Suite, celestia: s.celestia, simapp: s.simapp}
	return suite.SetupNoopISM(chain)
}

func (s *ForwardingPoCTestSuite) SetupMailBox(chain *ibctesting.TestChain, ismID util.HexAddress, domain uint32) util.HexAddress {
	suite := &HyperlaneTestSuite{Suite: s.Suite, celestia: s.celestia, simapp: s.simapp}
	return suite.SetupMailBox(chain, ismID, domain)
}

func (s *ForwardingPoCTestSuite) CreateCollateralToken(chain *ibctesting.TestChain, ismID, mailboxID util.HexAddress, denom string) util.HexAddress {
	suite := &HyperlaneTestSuite{Suite: s.Suite, celestia: s.celestia, simapp: s.simapp}
	return suite.CreateCollateralToken(chain, ismID, mailboxID, denom)
}

func (s *ForwardingPoCTestSuite) CreateSyntheticToken(chain *ibctesting.TestChain, ismID, mailboxID util.HexAddress) util.HexAddress {
	suite := &HyperlaneTestSuite{Suite: s.Suite, celestia: s.celestia, simapp: s.simapp}
	return suite.CreateSyntheticToken(chain, ismID, mailboxID)
}

func (s *ForwardingPoCTestSuite) EnrollRemoteRouter(chain *ibctesting.TestChain, tokenID util.HexAddress, domain uint32, recvContract string) {
	suite := &HyperlaneTestSuite{Suite: s.Suite, celestia: s.celestia, simapp: s.simapp}
	suite.EnrollRemoteRouter(chain, tokenID, domain, recvContract)
}
