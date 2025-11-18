package warp_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/x/warp"

	"github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// TestRouterEnrollmentIntegration tests router enrollment end-to-end
// This test verifies that Issue #3 (routes not being stored) is actually happening
// NOTE: This test is incomplete. Use test/interop/hyperlane_permissionless_simple_test.go instead
func TestRouterEnrollmentIntegration(t *testing.T) {
	t.Skip("Incomplete test - use test/interop/hyperlane_permissionless_simple_test.go instead")

	// Create a minimal app instance
	db := dbm.NewMemDB()
	testApp := app.New(
		nil, // logger
		db,  // db
		nil, // trace store
		0,   // invCheckPeriod
		sims.EmptyAppOptions{}, // appOptions
	)

	// Create context
	ctx := testApp.NewContext(false)

	// Get module address
	moduleAddr := authtypes.NewModuleAddress("warp").String()
	t.Logf("Module address: %s", moduleAddr)

	// Create test accounts
	user1Addr := sdk.AccAddress("user1_______________").String()

	// Initialize permissionless enrollment handler
	permissionless := warp.NewPermissionlessEnrollment(&testApp.WarpKeeper, "warp")

	// Step 1: Create a mailbox (module-owned)
	mailboxID := util.HexAddress{0x68, 0x79, 0x70, 0x65, 0x72, 0x6c, 0x61, 0x6e, 0x65} // "hyperlane"

	// Step 2: Create a module-owned token
	tokenID := util.HexAddress{0x72, 0x6f, 0x75, 0x74, 0x65, 0x72, 0x5f, 0x61, 0x70, 0x70} // "router_app"
	token := warptypes.HypToken{
		Id:            tokenID,
		Owner:         moduleAddr, // Module-owned - should allow permissionless enrollment!
		TokenType:     warptypes.HYP_TOKEN_TYPE_COLLATERAL,
		OriginMailbox: mailboxID,
		OriginDenom:   "utia",
	}

	err := testApp.WarpKeeper.HypTokens.Set(ctx, tokenID.GetInternalId(), token)
	require.NoError(t, err, "Failed to create token")
	t.Logf("✓ Created token with ID: %s", tokenID.String())

	// Step 3: User1 enrolls Ethereum route (domain 1)
	ethRouter := "0x000000000000000000000000aF9053bB6c4346381C77C2FeD279B17ABAfCDf4d"

	msg := &warptypes.MsgEnrollRemoteRouter{
		TokenId: tokenID,
		Owner:   user1Addr, // Random user - should work since token is module-owned!
		RemoteRouter: &warptypes.RemoteRouter{
			ReceiverDomain:   1, // Ethereum
			ReceiverContract: ethRouter,
			Gas:              math.NewInt(200000),
		},
	}

	t.Logf("Enrolling route for domain %d with router %s", msg.RemoteRouter.ReceiverDomain, msg.RemoteRouter.ReceiverContract)

	// Call the permissionless enrollment handler
	resp, err := permissionless.EnrollRemoteRouterPermissionless(ctx, msg)
	require.NoError(t, err, "Route enrollment should succeed for module-owned token")
	require.NotNil(t, resp, "Response should not be nil")
	t.Logf("✓ Route enrollment succeeded")

	// Step 4: VERIFY THE ROUTE WAS ACTUALLY STORED (This is the bug check!)
	route, err := testApp.WarpKeeper.EnrolledRouters.Get(
		ctx,
		collections.Join(tokenID.GetInternalId(), uint32(1)),
	)

	if err != nil {
		t.Fatalf("❌ BUG FOUND: Route was not stored! Error: %v", err)
	}

	if route.ReceiverContract != ethRouter {
		t.Fatalf("❌ BUG FOUND: Route stored with wrong address. Expected: %s, Got: %s",
			ethRouter, route.ReceiverContract)
	}

	t.Logf("✓ Route was stored correctly!")
	t.Logf("  Domain: %d", 1)
	t.Logf("  Router: %s", route.ReceiverContract)
	t.Logf("  Gas: %s", route.Gas.String())

	// Step 5: Verify we can query the route
	allRouters, err := testApp.WarpKeeper.EnrolledRouters.Iterate(
		ctx,
		collections.NewPrefixedPairRange[uint64, uint32](tokenID.GetInternalId()),
	)
	require.NoError(t, err, "Failed to iterate enrolled routers")
	defer allRouters.Close()

	count := 0
	for allRouters.Valid() {
		kv, err := allRouters.KeyValue()
		require.NoError(t, err)
		t.Logf("Found route: domain=%d, router=%s", kv.Key.K2(), kv.Value.ReceiverContract)
		count++
		allRouters.Next()
	}

	require.Equal(t, 1, count, "Should have exactly 1 route enrolled")
	t.Logf("✓ Query returned correct number of routes: %d", count)

	// Step 6: Test first-enrollment-wins (try to enroll duplicate)
	user2Addr := sdk.AccAddress("user2_______________").String()
	maliciousRouter := "0x0000000000000000000000000000000000000bad"

	msg2 := &warptypes.MsgEnrollRemoteRouter{
		TokenId: tokenID,
		Owner:   user2Addr,
		RemoteRouter: &warptypes.RemoteRouter{
			ReceiverDomain:   1, // Same domain!
			ReceiverContract: maliciousRouter,
			Gas:              math.NewInt(50000),
		},
	}

	_, err = permissionless.EnrollRemoteRouterPermissionless(ctx, msg2)
	require.Error(t, err, "Duplicate enrollment should fail")
	require.Contains(t, err.Error(), "route already enrolled", "Error should mention route already enrolled")
	t.Logf("✓ First-enrollment-wins protection working correctly")

	// Verify original route unchanged
	route, err = testApp.WarpKeeper.EnrolledRouters.Get(
		ctx,
		collections.Join(tokenID.GetInternalId(), uint32(1)),
	)
	require.NoError(t, err)
	require.Equal(t, ethRouter, route.ReceiverContract, "Original route should be unchanged")
	t.Logf("✓ Original route remains unchanged after duplicate attempt")

	t.Log("\n========================================")
	t.Log("ALL TESTS PASSED! Router enrollment is working correctly.")
	t.Log("========================================")
}
