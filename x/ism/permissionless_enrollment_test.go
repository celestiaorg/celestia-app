package ism_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/keeper"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"

	"github.com/celestiaorg/celestia-app/v6/x/ism"
)

type PermissionlessISMEnrollmentTestSuite struct {
	suite.Suite
	keeper                *ismkeeper.Keeper
	permissionless        *ism.PermissionlessISMEnrollment
	ctx                   sdk.Context
	moduleAddr            string
	userAddr              string
	moduleOwnedRoutingISM util.HexAddress
	userOwnedRoutingISM   util.HexAddress
	multisigISM1          util.HexAddress
	multisigISM2          util.HexAddress
}

func TestPermissionlessISMEnrollmentSuite(t *testing.T) {
	suite.Run(t, new(PermissionlessISMEnrollmentTestSuite))
}

func (suite *PermissionlessISMEnrollmentTestSuite) SetupTest() {
	// Setup test context and keeper
	// Note: This would need proper initialization in actual implementation

	suite.moduleAddr = authtypes.NewModuleAddress("hyperlane").String()
	suite.userAddr = sdk.AccAddress("user_address_______").String()

	// Initialize permissionless enrollment handler
	suite.permissionless = ism.NewPermissionlessISMEnrollment(suite.keeper, "hyperlane")
}

// TestSetRoutingIsmDomainPermissionless_ModuleOwned tests pure permissionless enrollment for module-owned RoutingISMs
func (suite *PermissionlessISMEnrollmentTestSuite) TestSetRoutingIsmDomainPermissionless_ModuleOwned() {
	t := suite.T()

	// Create a module-owned RoutingISM
	routingISM := &ismtypes.RoutingISM{
		Id:     suite.moduleOwnedRoutingISM,
		Owner:  suite.moduleAddr, // Module-owned
		Routes: []ismtypes.Route{},
	}

	err := suite.keeper.GetIsms().Set(suite.ctx, suite.moduleOwnedRoutingISM.GetInternalId(), routingISM)
	require.NoError(t, err)

	// Create a MultisigISM that can be routed to
	multisigISM := &ismtypes.MultisigISM{
		Id:        suite.multisigISM1,
		Owner:     suite.userAddr,
		Threshold: 3,
	}
	err = suite.keeper.GetIsms().Set(suite.ctx, suite.multisigISM1.GetInternalId(), multisigISM)
	require.NoError(t, err)

	// Anyone can set a route - use random address
	randomUser := sdk.AccAddress("random_user________").String()

	req := &ismtypes.MsgSetRoutingIsmDomain{
		IsmId: suite.moduleOwnedRoutingISM,
		Owner: randomUser, // Random user, not module!
		Route: ismtypes.Route{
			Domain: 118, // Osmosis
			Ism:    suite.multisigISM1,
		},
	}

	// Should succeed - pure permissionless!
	resp, err := suite.permissionless.SetRoutingIsmDomainPermissionless(suite.ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify route was set
	updatedISM, err := suite.keeper.GetIsms().Get(suite.ctx, suite.moduleOwnedRoutingISM.GetInternalId())
	require.NoError(t, err)

	routingISMUpdated, ok := updatedISM.(*ismtypes.RoutingISM)
	require.True(t, ok)
	require.Len(t, routingISMUpdated.Routes, 1)
	require.Equal(t, uint32(118), routingISMUpdated.Routes[0].Domain)
	require.Equal(t, suite.multisigISM1.String(), routingISMUpdated.Routes[0].Ism.String())
}

// TestSetRoutingIsmDomainPermissionless_UserOwned tests that user-owned RoutingISMs still require ownership
func (suite *PermissionlessISMEnrollmentTestSuite) TestSetRoutingIsmDomainPermissionless_UserOwned() {
	t := suite.T()

	// Create a user-owned RoutingISM
	routingISM := &ismtypes.RoutingISM{
		Id:     suite.userOwnedRoutingISM,
		Owner:  suite.userAddr, // User-owned
		Routes: []ismtypes.Route{},
	}

	err := suite.keeper.GetIsms().Set(suite.ctx, suite.userOwnedRoutingISM.GetInternalId(), routingISM)
	require.NoError(t, err)

	// Create a MultisigISM
	multisigISM := &ismtypes.MultisigISM{
		Id:        suite.multisigISM1,
		Owner:     suite.userAddr,
		Threshold: 3,
	}
	err = suite.keeper.GetIsms().Set(suite.ctx, suite.multisigISM1.GetInternalId(), multisigISM)
	require.NoError(t, err)

	// Random user tries to set route
	randomUser := sdk.AccAddress("random_user________").String()

	req := &ismtypes.MsgSetRoutingIsmDomain{
		IsmId: suite.userOwnedRoutingISM,
		Owner: randomUser,
		Route: ismtypes.Route{
			Domain: 118,
			Ism:    suite.multisigISM1,
		},
	}

	// Should fail - user-owned requires ownership
	resp, err := suite.permissionless.SetRoutingIsmDomainPermissionless(suite.ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "does not own RoutingISM")

	// Owner can still set route
	req.Owner = suite.userAddr
	resp, err = suite.permissionless.SetRoutingIsmDomainPermissionless(suite.ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// TestSetRoutingIsmDomainPermissionless_FirstEnrollmentWins tests that duplicate enrollments are prevented
func (suite *PermissionlessISMEnrollmentTestSuite) TestSetRoutingIsmDomainPermissionless_FirstEnrollmentWins() {
	t := suite.T()

	// Create a module-owned RoutingISM
	routingISM := &ismtypes.RoutingISM{
		Id:     suite.moduleOwnedRoutingISM,
		Owner:  suite.moduleAddr,
		Routes: []ismtypes.Route{},
	}

	err := suite.keeper.GetIsms().Set(suite.ctx, suite.moduleOwnedRoutingISM.GetInternalId(), routingISM)
	require.NoError(t, err)

	// Create two MultisigISMs
	legitimateISM := &ismtypes.MultisigISM{
		Id:        suite.multisigISM1,
		Owner:     suite.userAddr,
		Threshold: 3,
	}
	err = suite.keeper.GetIsms().Set(suite.ctx, suite.multisigISM1.GetInternalId(), legitimateISM)
	require.NoError(t, err)

	maliciousISM := &ismtypes.MultisigISM{
		Id:        suite.multisigISM2,
		Owner:     sdk.AccAddress("malicious_user____").String(),
		Threshold: 1, // Weak threshold
	}
	err = suite.keeper.GetIsms().Set(suite.ctx, suite.multisigISM2.GetInternalId(), maliciousISM)
	require.NoError(t, err)

	// First user enrolls
	user1 := sdk.AccAddress("user1_____________").String()
	req1 := &ismtypes.MsgSetRoutingIsmDomain{
		IsmId: suite.moduleOwnedRoutingISM,
		Owner: user1,
		Route: ismtypes.Route{
			Domain: 118,
			Ism:    suite.multisigISM1, // Legitimate ISM
		},
	}

	resp, err := suite.permissionless.SetRoutingIsmDomainPermissionless(suite.ctx, req1)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Second user tries to enroll for same domain
	user2 := sdk.AccAddress("user2_____________").String()
	req2 := &ismtypes.MsgSetRoutingIsmDomain{
		IsmId: suite.moduleOwnedRoutingISM,
		Owner: user2,
		Route: ismtypes.Route{
			Domain: 118, // Same domain!
			Ism:    suite.multisigISM2, // Malicious ISM
		},
	}

	// Should fail - first enrollment wins
	resp, err = suite.permissionless.SetRoutingIsmDomainPermissionless(suite.ctx, req2)
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "route already enrolled")
	require.Contains(t, err.Error(), "first-enrollment-wins")

	// Verify original route is unchanged
	updatedISM, err := suite.keeper.GetIsms().Get(suite.ctx, suite.moduleOwnedRoutingISM.GetInternalId())
	require.NoError(t, err)

	routingISMUpdated, ok := updatedISM.(*ismtypes.RoutingISM)
	require.True(t, ok)
	require.Len(t, routingISMUpdated.Routes, 1)
	require.Equal(t, suite.multisigISM1.String(), routingISMUpdated.Routes[0].Ism.String())
}

// TestSetRoutingIsmDomainPermissionless_MultipleDomains tests enrolling multiple different domains
func (suite *PermissionlessISMEnrollmentTestSuite) TestSetRoutingIsmDomainPermissionless_MultipleDomains() {
	t := suite.T()

	// Create a module-owned RoutingISM
	routingISM := &ismtypes.RoutingISM{
		Id:     suite.moduleOwnedRoutingISM,
		Owner:  suite.moduleAddr,
		Routes: []ismtypes.Route{},
	}

	err := suite.keeper.GetIsms().Set(suite.ctx, suite.moduleOwnedRoutingISM.GetInternalId(), routingISM)
	require.NoError(t, err)

	// Create two MultisigISMs for different chains
	osmosisISM := &ismtypes.MultisigISM{
		Id:        suite.multisigISM1,
		Owner:     suite.userAddr,
		Threshold: 3,
	}
	err = suite.keeper.GetIsms().Set(suite.ctx, suite.multisigISM1.GetInternalId(), osmosisISM)
	require.NoError(t, err)

	ethereumISM := &ismtypes.MultisigISM{
		Id:        suite.multisigISM2,
		Owner:     suite.userAddr,
		Threshold: 5,
	}
	err = suite.keeper.GetIsms().Set(suite.ctx, suite.multisigISM2.GetInternalId(), ethereumISM)
	require.NoError(t, err)

	// Enroll Osmosis (domain 118)
	req1 := &ismtypes.MsgSetRoutingIsmDomain{
		IsmId: suite.moduleOwnedRoutingISM,
		Owner: sdk.AccAddress("user1_____________").String(),
		Route: ismtypes.Route{
			Domain: 118,
			Ism:    suite.multisigISM1,
		},
	}

	resp, err := suite.permissionless.SetRoutingIsmDomainPermissionless(suite.ctx, req1)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Enroll Ethereum (domain 1)
	req2 := &ismtypes.MsgSetRoutingIsmDomain{
		IsmId: suite.moduleOwnedRoutingISM,
		Owner: sdk.AccAddress("user2_____________").String(),
		Route: ismtypes.Route{
			Domain: 1,
			Ism:    suite.multisigISM2,
		},
	}

	resp, err = suite.permissionless.SetRoutingIsmDomainPermissionless(suite.ctx, req2)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify both routes exist
	updatedISM, err := suite.keeper.GetIsms().Get(suite.ctx, suite.moduleOwnedRoutingISM.GetInternalId())
	require.NoError(t, err)

	routingISMUpdated, ok := updatedISM.(*ismtypes.RoutingISM)
	require.True(t, ok)
	require.Len(t, routingISMUpdated.Routes, 2)

	// Verify Osmosis route
	var osmosisRoute *ismtypes.Route
	for _, route := range routingISMUpdated.Routes {
		if route.Domain == 118 {
			osmosisRoute = &route
			break
		}
	}
	require.NotNil(t, osmosisRoute)
	require.Equal(t, suite.multisigISM1.String(), osmosisRoute.Ism.String())

	// Verify Ethereum route
	var ethereumRoute *ismtypes.Route
	for _, route := range routingISMUpdated.Routes {
		if route.Domain == 1 {
			ethereumRoute = &route
			break
		}
	}
	require.NotNil(t, ethereumRoute)
	require.Equal(t, suite.multisigISM2.String(), ethereumRoute.Ism.String())
}

// TestTransferRoutingIsmOwnership tests transferring RoutingISM ownership to module
func (suite *PermissionlessISMEnrollmentTestSuite) TestTransferRoutingIsmOwnership() {
	t := suite.T()

	// Create a user-owned RoutingISM
	routingISM := &ismtypes.RoutingISM{
		Id:     suite.userOwnedRoutingISM,
		Owner:  suite.userAddr,
		Routes: []ismtypes.Route{},
	}

	err := suite.keeper.GetIsms().Set(suite.ctx, suite.userOwnedRoutingISM.GetInternalId(), routingISM)
	require.NoError(t, err)

	// Transfer to module
	err = suite.permissionless.TransferRoutingIsmOwnership(suite.ctx, suite.userOwnedRoutingISM, suite.userAddr)
	require.NoError(t, err)

	// Verify ownership changed
	updatedISM, err := suite.keeper.GetIsms().Get(suite.ctx, suite.userOwnedRoutingISM.GetInternalId())
	require.NoError(t, err)

	routingISMUpdated, ok := updatedISM.(*ismtypes.RoutingISM)
	require.True(t, ok)
	require.Equal(t, suite.moduleAddr, routingISMUpdated.Owner)

	// Now anyone can enroll routes
	multisigISM := &ismtypes.MultisigISM{
		Id:        suite.multisigISM1,
		Owner:     suite.userAddr,
		Threshold: 3,
	}
	err = suite.keeper.GetIsms().Set(suite.ctx, suite.multisigISM1.GetInternalId(), multisigISM)
	require.NoError(t, err)

	randomUser := sdk.AccAddress("random_user________").String()
	req := &ismtypes.MsgSetRoutingIsmDomain{
		IsmId: suite.userOwnedRoutingISM,
		Owner: randomUser,
		Route: ismtypes.Route{
			Domain: 118,
			Ism:    suite.multisigISM1,
		},
	}

	resp, err := suite.permissionless.SetRoutingIsmDomainPermissionless(suite.ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// TestTransferRoutingIsmOwnership_NotOwner tests that only owner can transfer ownership
func (suite *PermissionlessISMEnrollmentTestSuite) TestTransferRoutingIsmOwnership_NotOwner() {
	t := suite.T()

	// Create a user-owned RoutingISM
	routingISM := &ismtypes.RoutingISM{
		Id:     suite.userOwnedRoutingISM,
		Owner:  suite.userAddr,
		Routes: []ismtypes.Route{},
	}

	err := suite.keeper.GetIsms().Set(suite.ctx, suite.userOwnedRoutingISM.GetInternalId(), routingISM)
	require.NoError(t, err)

	// Random user tries to transfer
	randomUser := sdk.AccAddress("random_user________").String()
	err = suite.permissionless.TransferRoutingIsmOwnership(suite.ctx, suite.userOwnedRoutingISM, randomUser)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not own RoutingISM")

	// Verify ownership unchanged
	unchangedISM, err := suite.keeper.GetIsms().Get(suite.ctx, suite.userOwnedRoutingISM.GetInternalId())
	require.NoError(t, err)

	routingISMUnchanged, ok := unchangedISM.(*ismtypes.RoutingISM)
	require.True(t, ok)
	require.Equal(t, suite.userAddr, routingISMUnchanged.Owner)
}

// TestSetRoutingIsmDomainPermissionless_Ownerless tests enrollment for renounced ownership RoutingISMs
func (suite *PermissionlessISMEnrollmentTestSuite) TestSetRoutingIsmDomainPermissionless_Ownerless() {
	t := suite.T()

	// Create a RoutingISM with no owner (renounced)
	routingISM := &ismtypes.RoutingISM{
		Id:     suite.moduleOwnedRoutingISM,
		Owner:  "", // No owner
		Routes: []ismtypes.Route{},
	}

	err := suite.keeper.GetIsms().Set(suite.ctx, suite.moduleOwnedRoutingISM.GetInternalId(), routingISM)
	require.NoError(t, err)

	// Create a MultisigISM
	multisigISM := &ismtypes.MultisigISM{
		Id:        suite.multisigISM1,
		Owner:     suite.userAddr,
		Threshold: 3,
	}
	err = suite.keeper.GetIsms().Set(suite.ctx, suite.multisigISM1.GetInternalId(), multisigISM)
	require.NoError(t, err)

	// Anyone can enroll
	randomUser := sdk.AccAddress("random_user________").String()
	req := &ismtypes.MsgSetRoutingIsmDomain{
		IsmId: suite.moduleOwnedRoutingISM,
		Owner: randomUser,
		Route: ismtypes.Route{
			Domain: 118,
			Ism:    suite.multisigISM1,
		},
	}

	resp, err := suite.permissionless.SetRoutingIsmDomainPermissionless(suite.ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// TestSetRoutingIsmDomainPermissionless_NonExistentISM tests that routing to non-existent ISM fails
func (suite *PermissionlessISMEnrollmentTestSuite) TestSetRoutingIsmDomainPermissionless_NonExistentISM() {
	t := suite.T()

	// Create a module-owned RoutingISM
	routingISM := &ismtypes.RoutingISM{
		Id:     suite.moduleOwnedRoutingISM,
		Owner:  suite.moduleAddr,
		Routes: []ismtypes.Route{},
	}

	err := suite.keeper.GetIsms().Set(suite.ctx, suite.moduleOwnedRoutingISM.GetInternalId(), routingISM)
	require.NoError(t, err)

	// Try to route to non-existent ISM
	fakeISMId := util.HexAddress{} // Non-existent
	req := &ismtypes.MsgSetRoutingIsmDomain{
		IsmId: suite.moduleOwnedRoutingISM,
		Owner: sdk.AccAddress("user______________").String(),
		Route: ismtypes.Route{
			Domain: 118,
			Ism:    fakeISMId,
		},
	}

	// Should fail
	resp, err := suite.permissionless.SetRoutingIsmDomainPermissionless(suite.ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "not found")
}
