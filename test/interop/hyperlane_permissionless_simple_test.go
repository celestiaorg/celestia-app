package interop

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v6/app/params"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/suite"
)

type SimplePermissionlessTestSuite struct {
	HyperlaneTestSuite
}

func TestSimplePermissionlessSuite(t *testing.T) {
	suite.Run(t, new(SimplePermissionlessTestSuite))
}

// TestModuleOwnedWarpRoute tests that anyone can enroll warp routes for module-owned tokens
func (s *SimplePermissionlessTestSuite) TestModuleOwnedWarpRoute() {
	const (
		CelestiaDomainID = 69420
		SimappDomainID   = 1337
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)

	// Setup ISMs and mailboxes
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)

	ismIDSimapp := s.SetupNoopISM(s.simapp)
	_ = s.SetupMailBox(s.simapp, ismIDSimapp, SimappDomainID)

	// Create MODULE-OWNED collateral token
	moduleAddr := authtypes.NewModuleAddress("warp").String()

	msg := &warptypes.MsgCreateCollateralToken{
		Owner:         moduleAddr, // Module-owned!
		OriginMailbox: mailboxIDCelestia,
		OriginDenom:   params.BondDenom,
	}

	res, err := celestiaApp.WarpKeeper.CreateCollateralToken(s.celestia.GetContext(), msg)
	s.Require().NoError(err)
	collatTokenID := res

	// Create synthetic token on simapp
	synTokenID := s.CreateSyntheticToken(s.simapp, ismIDSimapp, mailboxIDCelestia)

	// Anyone (even sender, not module) can enroll route
	msgEnroll := &warptypes.MsgEnrollRemoteRouter{
		TokenId: collatTokenID,
		Owner:   s.celestia.SenderAccount.GetAddress().String(), // Not module!
		RemoteRouter: &warptypes.RemoteRouter{
			ReceiverDomain:   SimappDomainID,
			ReceiverContract: synTokenID.String(),
			Gas:              math.NewInt(100000),
		},
	}

	// Should succeed - pure permissionless!
	_, err = s.celestia.SendMsgs(msgEnroll)
	s.Require().NoError(err, "Module-owned token should allow permissionless enrollment")

	// Verify route was enrolled
	route, err := celestiaApp.WarpKeeper.EnrolledRouters.Get(
		s.celestia.GetContext(),
		collections.Join(collatTokenID.GetInternalId(), uint32(SimappDomainID)),
	)
	s.Require().NoError(err)
	s.Require().Equal(synTokenID.String(), route.ReceiverContract)
}

// TestUserOwnedWarpRoute tests that user-owned tokens work correctly (owner can enroll)
func (s *SimplePermissionlessTestSuite) TestUserOwnedWarpRoute() {
	const (
		CelestiaDomainID = 69420
		SimappDomainID   = 1337
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)

	// Setup ISMs and mailboxes
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)

	ismIDSimapp := s.SetupNoopISM(s.simapp)
	_ = s.SetupMailBox(s.simapp, ismIDSimapp, SimappDomainID)

	// Create USER-OWNED collateral token (traditional)
	collatTokenID := s.CreateCollateralToken(s.celestia, ismIDCelestia, mailboxIDCelestia, params.BondDenom)
	synTokenID := s.CreateSyntheticToken(s.simapp, ismIDSimapp, mailboxIDCelestia)

	// Owner can enroll
	msgEnroll := &warptypes.MsgEnrollRemoteRouter{
		TokenId: collatTokenID,
		Owner:   s.celestia.SenderAccount.GetAddress().String(),
		RemoteRouter: &warptypes.RemoteRouter{
			ReceiverDomain:   SimappDomainID,
			ReceiverContract: synTokenID.String(),
			Gas:              math.NewInt(100000),
		},
	}

	_, err := s.celestia.SendMsgs(msgEnroll)
	s.Require().NoError(err, "Owner should be able to enroll on user-owned token")

	// Verify route was enrolled
	route, err := celestiaApp.WarpKeeper.EnrolledRouters.Get(
		s.celestia.GetContext(),
		collections.Join(collatTokenID.GetInternalId(), uint32(SimappDomainID)),
	)
	s.Require().NoError(err)
	s.Require().Equal(synTokenID.String(), route.ReceiverContract)
}

// TestFirstEnrollmentWins tests that duplicate enrollments are prevented
func (s *SimplePermissionlessTestSuite) TestFirstEnrollmentWins() {
	const (
		CelestiaDomainID = 69420
		SimappDomainID   = 1337
	)

	celestiaApp := s.GetCelestiaApp(s.celestia)

	// Setup ISMs and mailboxes
	ismIDCelestia := s.SetupNoopISM(s.celestia)
	mailboxIDCelestia := s.SetupMailBox(s.celestia, ismIDCelestia, CelestiaDomainID)

	ismIDSimapp := s.SetupNoopISM(s.simapp)
	_ = s.SetupMailBox(s.simapp, ismIDSimapp, SimappDomainID)

	// Create MODULE-OWNED collateral token
	moduleAddr := authtypes.NewModuleAddress("warp").String()

	msg := &warptypes.MsgCreateCollateralToken{
		Owner:         moduleAddr,
		OriginMailbox: mailboxIDCelestia,
		OriginDenom:   params.BondDenom,
	}

	collatTokenID, err := celestiaApp.WarpKeeper.CreateCollateralToken(s.celestia.GetContext(), msg)
	s.Require().NoError(err)

	// Create two synthetic tokens
	legitimateTokenID := s.CreateSyntheticToken(s.simapp, ismIDSimapp, mailboxIDCelestia)
	maliciousTokenID := s.CreateSyntheticToken(s.simapp, ismIDSimapp, mailboxIDCelestia)

	// First user (sender) enrolls legitimate route
	msgEnroll1 := &warptypes.MsgEnrollRemoteRouter{
		TokenId: collatTokenID,
		Owner:   s.celestia.SenderAccount.GetAddress().String(),
		RemoteRouter: &warptypes.RemoteRouter{
			ReceiverDomain:   SimappDomainID,
			ReceiverContract: legitimateTokenID.String(),
			Gas:              math.NewInt(100000),
		},
	}

	_, err = s.celestia.SendMsgs(msgEnroll1)
	s.Require().NoError(err, "First enrollment should succeed")

	// Second attempt (same or different user) tries to enroll malicious route for same domain
	msgEnroll2 := &warptypes.MsgEnrollRemoteRouter{
		TokenId: collatTokenID,
		Owner:   s.celestia.SenderAccount.GetAddress().String(),
		RemoteRouter: &warptypes.RemoteRouter{
			ReceiverDomain:   SimappDomainID, // Same domain!
			ReceiverContract: maliciousTokenID.String(),
			Gas:              math.NewInt(50000),
		},
	}

	// Should fail - first enrollment wins
	_, err = s.celestia.SendMsgs(msgEnroll2)
	s.Require().Error(err, "Second enrollment should fail (first-enrollment-wins)")
	s.Require().Contains(err.Error(), "already enrolled")

	// Verify original route is unchanged
	route, err := celestiaApp.WarpKeeper.EnrolledRouters.Get(
		s.celestia.GetContext(),
		collections.Join(collatTokenID.GetInternalId(), uint32(SimappDomainID)),
	)
	s.Require().NoError(err)
	s.Require().Equal(legitimateTokenID.String(), route.ReceiverContract, "Route should remain the legitimate one")
}
