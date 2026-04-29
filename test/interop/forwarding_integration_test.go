package interop

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v9/app/params"
	forwardingtypes "github.com/celestiaorg/celestia-app/v9/x/forwarding/types"
	minttypes "github.com/celestiaorg/celestia-app/v9/x/mint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/stretchr/testify/suite"
)

// Test domain IDs used across forwarding integration tests.
const (
	TestCelestiaDomainID uint32 = 69420
	TestChainADomainID   uint32 = 1337
	TestChainBDomainID   uint32 = 2222
	TestUnknownDomainID  uint32 = 99999
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

	hypMsg := ExtractDispatchMessage(res.Events)
	s.Require().NotEmpty(hypMsg, "should have hyperlane dispatch message")

	_, err = dstChain.SendMsgs(&coretypes.MsgProcessMessage{
		MailboxId: dstMailboxID,
		Relayer:   dstChain.SenderAccount.GetAddress().String(),
		Message:   hypMsg,
	})
	s.Require().NoError(err)
}

func (s *ForwardingIntegrationTestSuite) deriveForwardAddress(destDomain uint32, destRecipient []byte, tokenID util.HexAddress) sdk.AccAddress {
	forwardAddr, err := forwardingtypes.DeriveForwardingAddress(destDomain, destRecipient, tokenID.Bytes())
	s.Require().NoError(err)
	return sdk.AccAddress(forwardAddr)
}

func (s *ForwardingIntegrationTestSuite) bankDenomForToken(chain *ibctesting.TestChain, tokenID util.HexAddress) string {
	app := s.GetCelestiaApp(chain)
	token, err := app.WarpKeeper.HypTokens.Get(chain.GetContext(), tokenID.GetInternalId())
	s.Require().NoError(err)

	denom, err := app.ForwardingKeeper.BankDenomForToken(token)
	s.Require().NoError(err)
	return denom
}

func (s *ForwardingIntegrationTestSuite) newForwardMsg(forwardAddr sdk.AccAddress, destDomain uint32, destRecipient []byte, tokenID util.HexAddress) *forwardingtypes.MsgForward {
	return forwardingtypes.NewMsgForward(
		s.celestia.SenderAccount.GetAddress().String(),
		forwardAddr.String(),
		destDomain,
		RecipientToHex(destRecipient).String(),
		tokenID.String(),
		sdk.NewCoin("utia", math.NewInt(0)),
	)
}

func (s *ForwardingIntegrationTestSuite) TestBankDenomForTokenTIA() {
	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set up hyperlane infrastructure
	ismID := s.SetupNoopISM(s.celestia)
	mailboxID := s.SetupMailBox(s.celestia, ismID, TestCelestiaDomainID)

	// Create a collateral token for utia (TIA)
	collatTokenID := s.CreateCollateralToken(s.celestia, ismID, mailboxID, params.BondDenom)

	// Set up chainA counterparty and enroll router so TIA has a route
	ismIDChainA := s.SetupNoopISM(s.chainA)
	s.SetupMailBox(s.chainA, ismIDChainA, TestChainADomainID)
	tiaSynTokenID := s.CreateSyntheticToken(s.chainA, ismIDChainA, mailboxID)
	s.EnrollRemoteRouter(s.celestia, collatTokenID, TestChainADomainID, tiaSynTokenID.String())

	hypToken, err := celestiaApp.WarpKeeper.HypTokens.Get(ctx, collatTokenID.GetInternalId())
	s.Require().NoError(err)

	denom, err := celestiaApp.ForwardingKeeper.BankDenomForToken(hypToken)
	s.Require().NoError(err)
	s.Equal(warptypes.HYP_TOKEN_TYPE_COLLATERAL, hypToken.TokenType)
	s.Equal(params.BondDenom, denom)
}

func (s *ForwardingIntegrationTestSuite) TestBankDenomForTokenSynthetic() {
	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set up hyperlane infrastructure on chainA (origin chain)
	ismIDChainA := s.SetupNoopISM(s.chainA)
	mailboxIDChainA := s.SetupMailBox(s.chainA, ismIDChainA, TestChainADomainID)

	// Set up celestia
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	s.SetupMailBox(s.celestia, ismIDCelestia, TestCelestiaDomainID)

	// Create a synthetic token on celestia (representing a token from chainA)
	synTokenID := s.CreateSyntheticToken(s.celestia, ismIDCelestia, mailboxIDChainA)

	hypToken, err := celestiaApp.WarpKeeper.HypTokens.Get(ctx, synTokenID.GetInternalId())
	s.Require().NoError(err)

	denom, err := celestiaApp.ForwardingKeeper.BankDenomForToken(hypToken)
	s.Require().NoError(err)

	s.Equal(warptypes.HYP_TOKEN_TYPE_SYNTHETIC, hypToken.TokenType)
	s.Equal("hyperlane/"+synTokenID.String(), denom)
}

func (s *ForwardingIntegrationTestSuite) TestHasEnrolledRouter() {
	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set up hyperlane infrastructure
	ismID := s.SetupNoopISM(s.celestia)
	mailboxID := s.SetupMailBox(s.celestia, ismID, TestCelestiaDomainID)
	collatTokenID := s.CreateCollateralToken(s.celestia, ismID, mailboxID, params.BondDenom)

	// Before enrollment - should return false
	hasRouteBefore, err := celestiaApp.ForwardingKeeper.HasEnrolledRouter(ctx, collatTokenID, TestChainADomainID)
	s.Require().NoError(err)
	s.False(hasRouteBefore, "should NOT have enrolled router before enrollment")

	// Set up chainA and enroll router
	ismIDChainA := s.SetupNoopISM(s.chainA)
	s.SetupMailBox(s.chainA, ismIDChainA, TestChainADomainID)
	synTokenID := s.CreateSyntheticToken(s.chainA, ismIDChainA, mailboxID)
	s.EnrollRemoteRouter(s.celestia, collatTokenID, TestChainADomainID, synTokenID.String())

	// After enrollment - should return true
	hasRouteAfter, err := celestiaApp.ForwardingKeeper.HasEnrolledRouter(ctx, collatTokenID, TestChainADomainID)
	s.Require().NoError(err)
	s.True(hasRouteAfter, "should have enrolled router after enrollment")

	// Non-existent domain - should return false
	hasNonExistent, err := celestiaApp.ForwardingKeeper.HasEnrolledRouter(ctx, collatTokenID, 99999)
	s.Require().NoError(err)
	s.False(hasNonExistent, "should NOT have router for non-existent domain")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForwardFullFlow() {
	celestiaApp := s.GetCelestiaApp(s.celestia)
	ctx := s.celestia.GetContext()

	// Set up hyperlane infrastructure
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, TestCelestiaDomainID)
	collatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)

	// Set up chainA counterparty
	ismIDChainA := s.SetupNoopISM(s.chainA)
	mailboxIDChainA := s.SetupMailBox(s.chainA, ismIDChainA, TestChainADomainID)
	synTokenID := s.CreateSyntheticToken(s.chainA, ismIDChainA, mailboxIDCelestia)

	// Enroll routers
	s.EnrollRemoteRouter(s.celestia, collatTokenID, TestChainADomainID, synTokenID.String())
	s.EnrollRemoteRouter(s.chainA, synTokenID, TestCelestiaDomainID, collatTokenID.String())

	// Create destination recipient and derive forwarding address
	destRecipient := MakeRecipient32(s.chainA.SenderAccount.GetAddress())
	forwardAddr := s.deriveForwardAddress(TestChainADomainID, destRecipient, collatTokenID)

	// Fund the forwarding address
	fundAmount := math.NewInt(1000)
	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(params.BondDenom, fundAmount))

	// Verify forward address has funds
	forwardBalance := celestiaApp.BankKeeper.GetBalance(ctx, forwardAddr, params.BondDenom)
	s.Equal(fundAmount.Int64(), forwardBalance.Amount.Int64())

	// Create and execute MsgForward
	msg := s.newForwardMsg(forwardAddr, TestChainADomainID, destRecipient, collatTokenID)

	res, err := s.celestia.SendMsgs(msg)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	// Verify forward address is now empty
	newForwardBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	s.True(newForwardBalance.Amount.IsZero(), "forward address should be empty after forwarding")

	// If we found a dispatch event, verify the message can be processed on chainA
	hypMsg := ExtractDispatchMessage(res.Events)
	if hypMsg != "" {
		res, err = s.chainA.SendMsgs(&coretypes.MsgProcessMessage{
			MailboxId: mailboxIDChainA,
			Relayer:   s.chainA.SenderAccount.GetAddress().String(),
			Message:   hypMsg,
		})
		if err == nil {
			s.Require().NotNil(res)

			// Verify tokens arrived at destination
			chainAApp := s.GetSimapp(s.chainA)
			hypDenom, err := chainAApp.WarpKeeper.HypTokens.Get(s.chainA.GetContext(), synTokenID.GetInternalId())
			s.Require().NoError(err)

			destBalance := chainAApp.BankKeeper.GetBalance(s.chainA.GetContext(), s.chainA.SenderAccount.GetAddress(), "hyperlane/"+hypDenom.Id.String())
			s.Equal(fundAmount.Int64(), destBalance.Amount.Int64())
		}
	}
}

func (s *ForwardingIntegrationTestSuite) TestMsgForwardAddressMismatch() {
	randomAddr := sdk.AccAddress([]byte("random_address______"))
	destRecipient := MakeRecipient32(s.chainA.SenderAccount.GetAddress())

	msg := forwardingtypes.NewMsgForward(
		s.celestia.SenderAccount.GetAddress().String(),
		randomAddr.String(),
		1337,
		RecipientToHex(destRecipient).String(),
		"0x726f757465725f61707000000000000000000000000000010000000000000000",
		sdk.NewCoin("utia", math.NewInt(0)),
	)

	_, err := s.celestia.SendMsgs(msg)
	s.Require().Error(err)
	s.Contains(err.Error(), "derived address does not match")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForwardNoBalance() {
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, TestCelestiaDomainID)
	collatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)

	ismIDChainA := s.SetupNoopISM(s.chainA)
	s.SetupMailBox(s.chainA, ismIDChainA, 1337)
	tiaSynTokenID := s.CreateSyntheticToken(s.chainA, ismIDChainA, mailboxIDCelestia)
	s.EnrollRemoteRouter(s.celestia, collatTokenID, 1337, tiaSynTokenID.String())

	destRecipient := MakeRecipient32(s.chainA.SenderAccount.GetAddress())
	forwardAddr := s.deriveForwardAddress(1337, destRecipient, collatTokenID)

	msg := s.newForwardMsg(forwardAddr, 1337, destRecipient, collatTokenID)

	_, err := s.celestia.SendMsgs(msg)
	s.Require().Error(err)
	s.Contains(err.Error(), "no balance")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForwardLeavesUnrelatedTokenUntouched() {
	celestiaApp := s.GetCelestiaApp(s.celestia)

	// Set up hyperlane infrastructure on Celestia
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, TestCelestiaDomainID)
	tiaCollatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)

	// Set up chainA counterparty
	ismIDChainA := s.SetupNoopISM(s.chainA)
	mailboxIDChainA := s.SetupMailBox(s.chainA, ismIDChainA, TestChainADomainID)
	tiaSynTokenID := s.CreateSyntheticToken(s.chainA, ismIDChainA, mailboxIDCelestia)

	// Create second token pair: chainA collateral -> Celestia synthetic
	chainACollatTokenID := s.CreateCollateralToken(s.chainA, ismIDChainA, mailboxIDChainA, sdk.DefaultBondDenom)
	celestiaSynTokenID := s.CreateSyntheticToken(s.celestia, ismIDCelestia, mailboxIDChainA)

	// Enroll routers for both token pairs
	s.EnrollRemoteRouter(s.celestia, tiaCollatTokenID, TestChainADomainID, tiaSynTokenID.String())
	s.EnrollRemoteRouter(s.chainA, tiaSynTokenID, TestCelestiaDomainID, tiaCollatTokenID.String())
	s.EnrollRemoteRouter(s.chainA, chainACollatTokenID, TestCelestiaDomainID, celestiaSynTokenID.String())
	s.EnrollRemoteRouter(s.celestia, celestiaSynTokenID, TestChainADomainID, chainACollatTokenID.String())

	// Create destination recipient and derive forwarding address bound to TIA.
	destRecipient := MakeRecipient32(s.chainA.SenderAccount.GetAddress())
	forwardAddr := s.deriveForwardAddress(TestChainADomainID, destRecipient, tiaCollatTokenID)

	// Fund forward address with TIA
	tiaAmount := math.NewInt(1000)
	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(params.BondDenom, tiaAmount))

	// Warp transfer stake from chainA to forwardAddr on Celestia
	forwardAddrBytes := MakeRecipient32(forwardAddr)
	s.processWarpMessage(s.chainA, s.celestia, mailboxIDCelestia, &warptypes.MsgRemoteTransfer{
		Sender:            s.chainA.SenderAccount.GetAddress().String(),
		TokenId:           chainACollatTokenID,
		DestinationDomain: TestCelestiaDomainID,
		Recipient:         util.HexAddress(forwardAddrBytes),
		Amount:            math.NewInt(500),
	})

	syntheticDenom := s.bankDenomForToken(s.celestia, celestiaSynTokenID)

	// Verify forward address has both tokens, but is only bound to TIA.
	tiaBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	synBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, syntheticDenom)
	s.Equal(tiaAmount.Int64(), tiaBalance.Amount.Int64())
	s.Equal(int64(500), synBalance.Amount.Int64())

	msg := s.newForwardMsg(forwardAddr, TestChainADomainID, destRecipient, tiaCollatTokenID)

	res, err := s.celestia.SendMsgs(msg)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	// Verify only the bound TIA balance is consumed.
	newTiaBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	newSynBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, syntheticDenom)
	s.True(newTiaBalance.Amount.IsZero(), "TIA balance should be zero after forwarding")
	s.Equal(int64(500), newSynBalance.Amount.Int64(), "unrelated synthetic balance should remain at forwardAddr")

	s.Equal(1, CountDispatchEvents(res.Events), "should have 1 dispatch event for the bound token")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForwardIgnoresUnsupportedUnrelatedBalance() {
	celestiaApp := s.GetCelestiaApp(s.celestia)

	// Set up hyperlane infrastructure
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, TestCelestiaDomainID)
	tiaCollatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)

	// Set up chainA counterparty
	ismIDChainA := s.SetupNoopISM(s.chainA)
	_ = s.SetupMailBox(s.chainA, ismIDChainA, TestChainADomainID)
	tiaSynTokenID := s.CreateSyntheticToken(s.chainA, ismIDChainA, mailboxIDCelestia)

	// Enroll routers for TIA only
	s.EnrollRemoteRouter(s.celestia, tiaCollatTokenID, TestChainADomainID, tiaSynTokenID.String())
	s.EnrollRemoteRouter(s.chainA, tiaSynTokenID, TestCelestiaDomainID, tiaCollatTokenID.String())

	// Create destination and derive forwarding address bound to TIA.
	destRecipient := MakeRecipient32(s.chainA.SenderAccount.GetAddress())
	forwardAddr := s.deriveForwardAddress(TestChainADomainID, destRecipient, tiaCollatTokenID)

	// Fund with TIA (supported) and an unsupported IBC denom
	tiaAmount := math.NewInt(1000)
	unsupportedDenom := "ibc/ABC123UNSUPPORTED"
	unsupportedAmount := math.NewInt(500)

	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(params.BondDenom, tiaAmount))
	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(unsupportedDenom, unsupportedAmount))

	msg := s.newForwardMsg(forwardAddr, TestChainADomainID, destRecipient, tiaCollatTokenID)

	res, err := s.celestia.SendMsgs(msg)
	s.Require().NoError(err, "transaction should succeed and ignore unrelated unsupported balances")
	s.Require().NotNil(res)

	// Verify: TIA should be drained, unsupported should remain
	newTiaBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	newUnsupportedBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, unsupportedDenom)

	s.True(newTiaBalance.Amount.IsZero(), "TIA should be forwarded")
	s.Equal(unsupportedAmount.Int64(), newUnsupportedBalance.Amount.Int64(), "unsupported token should remain at forwardAddr")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForwardNoRouteForBoundToken() {
	// OtherDomainID is distinct from TestChainADomainID and TestChainBDomainID
	const OtherDomainID uint32 = 9999

	celestiaApp := s.GetCelestiaApp(s.celestia)

	// Set up hyperlane infrastructure on Celestia
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, TestCelestiaDomainID)
	tiaCollatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)

	// Create test token with route to OtherDomainID (NOT TestChainADomainID)
	testDenom := "uother"
	testCollatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, testDenom)

	// Set up chainA counterparty for TIA
	ismIDChainA := s.SetupNoopISM(s.chainA)
	_ = s.SetupMailBox(s.chainA, ismIDChainA, TestChainADomainID)
	tiaSynTokenID := s.CreateSyntheticToken(s.chainA, ismIDChainA, mailboxIDCelestia)

	// Enroll TIA route to chainA
	s.EnrollRemoteRouter(s.celestia, tiaCollatTokenID, TestChainADomainID, tiaSynTokenID.String())
	s.EnrollRemoteRouter(s.chainA, tiaSynTokenID, TestCelestiaDomainID, tiaCollatTokenID.String())

	// Enroll test token route to OTHER domain only (NOT chainA)
	s.EnrollRemoteRouter(s.celestia, testCollatTokenID, OtherDomainID, "0x0000000000000000000000000000000000000000000000000000000000000001")

	// Derive forwarding address for TestChainADomainID, but bind it to the token with no route.
	destRecipient := MakeRecipient32(s.chainA.SenderAccount.GetAddress())
	forwardAddr := s.deriveForwardAddress(TestChainADomainID, destRecipient, testCollatTokenID)

	testAmount := math.NewInt(500)
	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(testDenom, testAmount))

	msg := s.newForwardMsg(forwardAddr, TestChainADomainID, destRecipient, testCollatTokenID)

	_, err := s.celestia.SendMsgs(msg)
	s.Require().Error(err, "transaction should fail when the bound token has no route")
	s.Contains(err.Error(), "no warp route to destination domain")

	newTestBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, testDenom)
	s.Equal(testAmount.Int64(), newTestBalance.Amount.Int64(), "bound token should remain when forwarding fails")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForwardFullE2ESourceCollateralToken() {
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

	// Compute forward address on Celestia bound to the synthetic token held there.
	destRecipient := MakeRecipient32(s.chainB.SenderAccount.GetAddress())
	forwardAddr := s.deriveForwardAddress(ChainBDomainID, destRecipient, celestiaSynTokenID)

	// Warp transfer from ChainA to forwardAddr on Celestia
	forwardAddrBytes := MakeRecipient32(forwardAddr)
	s.processWarpMessage(s.chainA, s.celestia, mailboxIDCelestia, &warptypes.MsgRemoteTransfer{
		Sender:            s.chainA.SenderAccount.GetAddress().String(),
		TokenId:           chainACollatTokenID,
		DestinationDomain: CelestiaDomainID,
		Recipient:         util.HexAddress(forwardAddrBytes),
		Amount:            math.NewInt(1000),
	})

	// Verify synthetic tokens arrived at forwardAddr
	syntheticDenom := s.bankDenomForToken(s.celestia, celestiaSynTokenID)

	forwardBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, syntheticDenom)
	s.Equal(int64(1000), forwardBalance.Amount.Int64(), "synthetic tokens should arrive at forwardAddr")

	// Execute forwarding on Celestia
	forwardMsg := s.newForwardMsg(forwardAddr, ChainBDomainID, destRecipient, celestiaSynTokenID)

	res, err := s.celestia.SendMsgs(forwardMsg)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	hypMsgToChainB := ExtractDispatchMessage(res.Events)
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

	finalBalance := chainBApp.BankKeeper.GetBalance(s.chainB.GetContext(), s.chainB.SenderAccount.GetAddress(), "hyperlane/"+chainBSynToken.Id.String())
	s.Equal(int64(1000), finalBalance.Amount.Int64(), "tokens should arrive at final destination on chainB")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForwardFullE2ETIASyntheticOnSource() {
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
	chainARecipient := MakeRecipient32(s.chainA.SenderAccount.GetAddress())
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
	chainATIABalance := chainAApp.BankKeeper.GetBalance(s.chainA.GetContext(), s.chainA.SenderAccount.GetAddress(), "hyperlane/"+chainATIAToken.Id.String())
	s.Equal(int64(2000), chainATIABalance.Amount.Int64())

	// Compute forward address on Celestia for ChainB, bound to native TIA that will be released there.
	destRecipient := MakeRecipient32(s.chainB.SenderAccount.GetAddress())
	forwardAddr := s.deriveForwardAddress(ChainBDomainID, destRecipient, tiaCollatTokenID)

	// Warp TIA synthetic back to forwardAddr on Celestia (releases collateral)
	forwardAddrBytes := MakeRecipient32(forwardAddr)
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
	forwardMsg := s.newForwardMsg(forwardAddr, ChainBDomainID, destRecipient, tiaCollatTokenID)

	res, err := s.celestia.SendMsgs(forwardMsg)
	s.Require().NoError(err)

	hypMsgToChainB := ExtractDispatchMessage(res.Events)

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

	finalBalance := chainBApp.BankKeeper.GetBalance(s.chainB.GetContext(), s.chainB.SenderAccount.GetAddress(), "hyperlane/"+chainBTIAToken.Id.String())
	s.Equal(int64(1000), finalBalance.Amount.Int64(), "synthetic TIA should arrive at final destination")
}

func (s *ForwardingIntegrationTestSuite) TestMsgForwardFullE2ECEXWithdrawal() {
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

	// Compute forward address on Celestia bound to native TIA.
	destRecipient := MakeRecipient32(s.chainB.SenderAccount.GetAddress())
	forwardAddr := s.deriveForwardAddress(ChainBDomainID, destRecipient, tiaCollatTokenID)

	// Simulate CEX withdrawal by directly funding the forward address
	cexWithdrawalAmount := math.NewInt(5000)
	s.fundAddress(s.celestia, forwardAddr, sdk.NewCoin(params.BondDenom, cexWithdrawalAmount))

	// Verify funds arrived
	forwardBalance := celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), forwardAddr, params.BondDenom)
	s.Equal(cexWithdrawalAmount.Int64(), forwardBalance.Amount.Int64())

	// Execute forwarding on Celestia
	forwardMsg := s.newForwardMsg(forwardAddr, ChainBDomainID, destRecipient, tiaCollatTokenID)

	res, err := s.celestia.SendMsgs(forwardMsg)
	s.Require().NoError(err)
	s.Require().NotNil(res)

	hypMsgToChainB := ExtractDispatchMessage(res.Events)
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

	finalBalance := chainBApp.BankKeeper.GetBalance(s.chainB.GetContext(), s.chainB.SenderAccount.GetAddress(), "hyperlane/"+chainBTIAToken.Id.String())
	s.Equal(cexWithdrawalAmount.Int64(), finalBalance.Amount.Int64(), "synthetic TIA should arrive at final destination")
}
