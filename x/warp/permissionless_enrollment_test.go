package warp_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"

	"github.com/celestiaorg/celestia-app/v6/x/warp"
)

type PermissionlessEnrollmentTestSuite struct {
	suite.Suite
	keeper              *warpkeeper.Keeper
	permissionless      *warp.PermissionlessEnrollment
	ctx                 sdk.Context
	moduleAddr          string
	userAddr            string
	moduleOwnedTokenId  util.HexAddress
	userOwnedTokenId    util.HexAddress
}

func TestPermissionlessEnrollmentSuite(t *testing.T) {
	suite.Run(t, new(PermissionlessEnrollmentTestSuite))
}

func (suite *PermissionlessEnrollmentTestSuite) SetupTest() {
	// Setup test context and keeper
	// Note: This would need proper initialization in actual implementation

	suite.moduleAddr = authtypes.NewModuleAddress("warp").String()
	suite.userAddr = sdk.AccAddress("user_address_______").String()

	// Initialize permissionless enrollment handler
	suite.permissionless = warp.NewPermissionlessEnrollment(suite.keeper, "warp")
}

// TestEnrollRemoteRouterPermissionless_ModuleOwned tests pure permissionless enrollment for module-owned tokens
func (suite *PermissionlessEnrollmentTestSuite) TestEnrollRemoteRouterPermissionless_ModuleOwned() {
	t := suite.T()

	// Create a module-owned token
	token := warptypes.HypToken{
		Id:            suite.moduleOwnedTokenId,
		Owner:         suite.moduleAddr, // Module-owned
		TokenType:     warptypes.HYP_TOKEN_TYPE_COLLATERAL,
		OriginMailbox: util.HexAddress{},
		OriginDenom:   "utia",
	}

	err := suite.keeper.HypTokens.Set(suite.ctx, suite.moduleOwnedTokenId.GetInternalId(), token)
	require.NoError(t, err)

	// Anyone can enroll a route - use random address
	randomUser := sdk.AccAddress("random_user________").String()

	msg := &warptypes.MsgEnrollRemoteRouter{
		TokenId: suite.moduleOwnedTokenId,
		Owner:   randomUser, // Random user, not module!
		RemoteRouter: &warptypes.RemoteRouter{
			ReceiverDomain:   118, // Osmosis
			ReceiverContract: "0xosmosis_synthetic_utia",
			Gas:              math.NewInt(100000),
		},
	}

	// Should succeed - pure permissionless!
	resp, err := suite.permissionless.EnrollRemoteRouterPermissionless(suite.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify route was enrolled
	route, err := suite.keeper.EnrolledRouters.Get(suite.ctx, collections.Join(suite.moduleOwnedTokenId.GetInternalId(), uint32(118)))
	require.NoError(t, err)
	require.Equal(t, "0xosmosis_synthetic_utia", route.ReceiverContract)
}

// TestEnrollRemoteRouterPermissionless_UserOwned tests that user-owned tokens still require ownership
func (suite *PermissionlessEnrollmentTestSuite) TestEnrollRemoteRouterPermissionless_UserOwned() {
	t := suite.T()

	// Create a user-owned token
	token := warptypes.HypToken{
		Id:            suite.userOwnedTokenId,
		Owner:         suite.userAddr, // User-owned
		TokenType:     warptypes.HYP_TOKEN_TYPE_COLLATERAL,
		OriginMailbox: util.HexAddress{},
		OriginDenom:   "utia",
	}

	err := suite.keeper.HypTokens.Set(suite.ctx, suite.userOwnedTokenId.GetInternalId(), token)
	require.NoError(t, err)

	// Random user tries to enroll
	randomUser := sdk.AccAddress("random_user________").String()

	msg := &warptypes.MsgEnrollRemoteRouter{
		TokenId: suite.userOwnedTokenId,
		Owner:   randomUser,
		RemoteRouter: &warptypes.RemoteRouter{
			ReceiverDomain:   118,
			ReceiverContract: "0xosmosis_synthetic_utia",
			Gas:              math.NewInt(100000),
		},
	}

	// Should fail - user-owned requires ownership
	resp, err := suite.permissionless.EnrollRemoteRouterPermissionless(suite.ctx, msg)
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "does not own token")

	// Owner can still enroll
	msg.Owner = suite.userAddr
	resp, err = suite.permissionless.EnrollRemoteRouterPermissionless(suite.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// TestEnrollRemoteRouterPermissionless_FirstEnrollmentWins tests that duplicate enrollments are prevented
func (suite *PermissionlessEnrollmentTestSuite) TestEnrollRemoteRouterPermissionless_FirstEnrollmentWins() {
	t := suite.T()

	// Create a module-owned token
	token := warptypes.HypToken{
		Id:            suite.moduleOwnedTokenId,
		Owner:         suite.moduleAddr,
		TokenType:     warptypes.HYP_TOKEN_TYPE_COLLATERAL,
		OriginMailbox: util.HexAddress{},
		OriginDenom:   "utia",
	}

	err := suite.keeper.HypTokens.Set(suite.ctx, suite.moduleOwnedTokenId.GetInternalId(), token)
	require.NoError(t, err)

	// First user enrolls
	user1 := sdk.AccAddress("user1_____________").String()
	msg1 := &warptypes.MsgEnrollRemoteRouter{
		TokenId: suite.moduleOwnedTokenId,
		Owner:   user1,
		RemoteRouter: &warptypes.RemoteRouter{
			ReceiverDomain:   118,
			ReceiverContract: "0xlegitimate_router",
			Gas:              math.NewInt(100000),
		},
	}

	resp, err := suite.permissionless.EnrollRemoteRouterPermissionless(suite.ctx, msg1)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Second user tries to enroll for same domain
	user2 := sdk.AccAddress("user2_____________").String()
	msg2 := &warptypes.MsgEnrollRemoteRouter{
		TokenId: suite.moduleOwnedTokenId,
		Owner:   user2,
		RemoteRouter: &warptypes.RemoteRouter{
			ReceiverDomain:   118, // Same domain!
			ReceiverContract: "0xmalicious_router",
			Gas:              math.NewInt(50000),
		},
	}

	// Should fail - first enrollment wins
	resp, err = suite.permissionless.EnrollRemoteRouterPermissionless(suite.ctx, msg2)
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "route already enrolled")
	require.Contains(t, err.Error(), "first-enrollment-wins")

	// Verify original route is unchanged
	route, err := suite.keeper.EnrolledRouters.Get(suite.ctx, collections.Join(suite.moduleOwnedTokenId.GetInternalId(), uint32(118)))
	require.NoError(t, err)
	require.Equal(t, "0xlegitimate_router", route.ReceiverContract)
}

// TestEnrollRemoteRouterPermissionless_MultipleDomains tests enrolling multiple different domains
func (suite *PermissionlessEnrollmentTestSuite) TestEnrollRemoteRouterPermissionless_MultipleDomains() {
	t := suite.T()

	// Create a module-owned token
	token := warptypes.HypToken{
		Id:            suite.moduleOwnedTokenId,
		Owner:         suite.moduleAddr,
		TokenType:     warptypes.HYP_TOKEN_TYPE_COLLATERAL,
		OriginMailbox: util.HexAddress{},
		OriginDenom:   "utia",
	}

	err := suite.keeper.HypTokens.Set(suite.ctx, suite.moduleOwnedTokenId.GetInternalId(), token)
	require.NoError(t, err)

	// Enroll Osmosis (domain 118)
	msg1 := &warptypes.MsgEnrollRemoteRouter{
		TokenId: suite.moduleOwnedTokenId,
		Owner:   sdk.AccAddress("user1_____________").String(),
		RemoteRouter: &warptypes.RemoteRouter{
			ReceiverDomain:   118,
			ReceiverContract: "0xosmosis_router",
			Gas:              math.NewInt(100000),
		},
	}

	resp, err := suite.permissionless.EnrollRemoteRouterPermissionless(suite.ctx, msg1)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Enroll Ethereum (domain 1)
	msg2 := &warptypes.MsgEnrollRemoteRouter{
		TokenId: suite.moduleOwnedTokenId,
		Owner:   sdk.AccAddress("user2_____________").String(),
		RemoteRouter: &warptypes.RemoteRouter{
			ReceiverDomain:   1,
			ReceiverContract: "0xethereum_router",
			Gas:              math.NewInt(200000),
		},
	}

	resp, err = suite.permissionless.EnrollRemoteRouterPermissionless(suite.ctx, msg2)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify both routes exist
	route1, err := suite.keeper.EnrolledRouters.Get(suite.ctx, collections.Join(suite.moduleOwnedTokenId.GetInternalId(), uint32(118)))
	require.NoError(t, err)
	require.Equal(t, "0xosmosis_router", route1.ReceiverContract)

	route2, err := suite.keeper.EnrolledRouters.Get(suite.ctx, collections.Join(suite.moduleOwnedTokenId.GetInternalId(), uint32(1)))
	require.NoError(t, err)
	require.Equal(t, "0xethereum_router", route2.ReceiverContract)
}

// TestTransferTokenOwnership tests transferring token ownership to module
func (suite *PermissionlessEnrollmentTestSuite) TestTransferTokenOwnership() {
	t := suite.T()

	// Create a user-owned token
	token := warptypes.HypToken{
		Id:            suite.userOwnedTokenId,
		Owner:         suite.userAddr,
		TokenType:     warptypes.HYP_TOKEN_TYPE_COLLATERAL,
		OriginMailbox: util.HexAddress{},
		OriginDenom:   "utia",
	}

	err := suite.keeper.HypTokens.Set(suite.ctx, suite.userOwnedTokenId.GetInternalId(), token)
	require.NoError(t, err)

	// Transfer to module
	err = suite.permissionless.TransferTokenOwnership(suite.ctx, suite.userOwnedTokenId, suite.userAddr)
	require.NoError(t, err)

	// Verify ownership changed
	updatedToken, err := suite.keeper.HypTokens.Get(suite.ctx, suite.userOwnedTokenId.GetInternalId())
	require.NoError(t, err)
	require.Equal(t, suite.moduleAddr, updatedToken.Owner)

	// Now anyone can enroll routes
	randomUser := sdk.AccAddress("random_user________").String()
	msg := &warptypes.MsgEnrollRemoteRouter{
		TokenId: suite.userOwnedTokenId,
		Owner:   randomUser,
		RemoteRouter: &warptypes.RemoteRouter{
			ReceiverDomain:   118,
			ReceiverContract: "0xosmosis_router",
			Gas:              math.NewInt(100000),
		},
	}

	resp, err := suite.permissionless.EnrollRemoteRouterPermissionless(suite.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// TestTransferTokenOwnership_NotOwner tests that only owner can transfer ownership
func (suite *PermissionlessEnrollmentTestSuite) TestTransferTokenOwnership_NotOwner() {
	t := suite.T()

	// Create a user-owned token
	token := warptypes.HypToken{
		Id:            suite.userOwnedTokenId,
		Owner:         suite.userAddr,
		TokenType:     warptypes.HYP_TOKEN_TYPE_COLLATERAL,
		OriginMailbox: util.HexAddress{},
		OriginDenom:   "utia",
	}

	err := suite.keeper.HypTokens.Set(suite.ctx, suite.userOwnedTokenId.GetInternalId(), token)
	require.NoError(t, err)

	// Random user tries to transfer
	randomUser := sdk.AccAddress("random_user________").String()
	err = suite.permissionless.TransferTokenOwnership(suite.ctx, suite.userOwnedTokenId, randomUser)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not own token")

	// Verify ownership unchanged
	unchangedToken, err := suite.keeper.HypTokens.Get(suite.ctx, suite.userOwnedTokenId.GetInternalId())
	require.NoError(t, err)
	require.Equal(t, suite.userAddr, unchangedToken.Owner)
}

// TestEnrollRemoteRouterPermissionless_Ownerless tests enrollment for renounced ownership tokens
func (suite *PermissionlessEnrollmentTestSuite) TestEnrollRemoteRouterPermissionless_Ownerless() {
	t := suite.T()

	// Create a token with no owner (renounced)
	token := warptypes.HypToken{
		Id:            suite.moduleOwnedTokenId,
		Owner:         "", // No owner
		TokenType:     warptypes.HYP_TOKEN_TYPE_COLLATERAL,
		OriginMailbox: util.HexAddress{},
		OriginDenom:   "utia",
	}

	err := suite.keeper.HypTokens.Set(suite.ctx, suite.moduleOwnedTokenId.GetInternalId(), token)
	require.NoError(t, err)

	// Anyone can enroll
	randomUser := sdk.AccAddress("random_user________").String()
	msg := &warptypes.MsgEnrollRemoteRouter{
		TokenId: suite.moduleOwnedTokenId,
		Owner:   randomUser,
		RemoteRouter: &warptypes.RemoteRouter{
			ReceiverDomain:   118,
			ReceiverContract: "0xosmosis_router",
			Gas:              math.NewInt(100000),
		},
	}

	resp, err := suite.permissionless.EnrollRemoteRouterPermissionless(suite.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
}
