package keeper_test

import (
	"context"
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/x/forwarding/keeper"
	"github.com/celestiaorg/celestia-app/v10/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NOTE: Full query server tests require warp infrastructure setup.
// These tests cover nil request handling and basic validation.
// Full integration tests are in test/interop/forwarding_integration_test.go

func TestQueryDeriveForwardingAddressNilRequest(t *testing.T) {
	// Create a minimal keeper (will panic on actual queries but nil checks happen first)
	k := keeper.Keeper{}
	queryServer := keeper.NewQueryServerImpl(k)

	_, err := queryServer.DeriveForwardingAddress(context.Background(), nil)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), "request cannot be nil")
}

func TestQueryQuoteForwardingFeeNilRequest(t *testing.T) {
	k := keeper.Keeper{}
	queryServer := keeper.NewQueryServerImpl(k)

	_, err := queryServer.QuoteForwardingFee(context.Background(), nil)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), "request cannot be nil")
}

func TestQueryDeriveForwardingAddressInvalidHex(t *testing.T) {
	testCases := []struct {
		name          string
		destRecipient string
	}{
		{"not hex", "not-a-hex-string"},
		{"invalid hex chars", "0xGGHHIIJJ"},
		{"too short", "0xdeadbeef"},
		{"too long", "0x" + strings.Repeat("ab", 33)},
	}

	k := keeper.Keeper{}
	queryServer := keeper.NewQueryServerImpl(k)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := queryServer.DeriveForwardingAddress(context.Background(), &types.QueryDeriveForwardingAddressRequest{
				DestDomain:    1,
				DestRecipient: tc.destRecipient,
				TokenId:       "0x726f757465725f61707000000000000000000000000000010000000000000000",
			})
			require.Error(t, err)
			// Invalid hex should return InvalidArgument
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestQueryDeriveForwardingAddressRequiresKnownTokenRoute(t *testing.T) {
	ctx := createTestContext()
	bankKeeper := NewMockBankKeeper()
	warpKeeper := NewMockWarpKeeper()
	hyperlaneKeeper := NewMockHyperlaneKeeper()

	token := createTestHypToken(1, appconsts.BondDenom, warptypes.HYP_TOKEN_TYPE_COLLATERAL)
	warpKeeper.Tokens = append(warpKeeper.Tokens, token)
	warpKeeper.EnrolledRouters[1] = map[uint32]warptypes.RemoteRouter{
		42161: {Gas: math.NewInt(200000)},
	}

	queryServer := keeper.NewQueryServerImpl(keeper.NewKeeper(bankKeeper, warpKeeper, hyperlaneKeeper))

	resp, err := queryServer.DeriveForwardingAddress(ctx, &types.QueryDeriveForwardingAddressRequest{
		DestDomain:    42161,
		DestRecipient: "0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		TokenId:       token.Id.String(),
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Address)

	_, err = queryServer.DeriveForwardingAddress(ctx, &types.QueryDeriveForwardingAddressRequest{
		DestDomain:    1111,
		DestRecipient: "0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		TokenId:       token.Id.String(),
	})
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))

	_, err = queryServer.DeriveForwardingAddress(ctx, &types.QueryDeriveForwardingAddressRequest{
		DestDomain:    42161,
		DestRecipient: "0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		TokenId:       "0x0000000000000000000000000000000000000000000000000000000000000009",
	})
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestQueryQuoteForwardingFeeRequiresExplicitToken(t *testing.T) {
	ctx := createTestContext()
	bankKeeper := NewMockBankKeeper()
	warpKeeper := NewMockWarpKeeper()
	hyperlaneKeeper := NewMockHyperlaneKeeper()
	hyperlaneKeeper.QuotedFee = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(77)))

	token := createTestHypToken(2, "uusdc", warptypes.HYP_TOKEN_TYPE_SYNTHETIC)
	warpKeeper.Tokens = append(warpKeeper.Tokens, token)
	warpKeeper.EnrolledRouters[2] = map[uint32]warptypes.RemoteRouter{
		999: {Gas: math.NewInt(250000)},
	}

	queryServer := keeper.NewQueryServerImpl(keeper.NewKeeper(bankKeeper, warpKeeper, hyperlaneKeeper))

	resp, err := queryServer.QuoteForwardingFee(ctx, &types.QueryQuoteForwardingFeeRequest{
		DestDomain: 999,
		TokenId:    token.Id.String(),
	})
	require.NoError(t, err)
	require.Equal(t, "77", resp.Fee.Amount.String())

	_, err = queryServer.QuoteForwardingFee(ctx, &types.QueryQuoteForwardingFeeRequest{
		DestDomain: 999,
		TokenId:    "0xdeadbeef",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = queryServer.QuoteForwardingFee(ctx, &types.QueryQuoteForwardingFeeRequest{
		DestDomain: 1000,
		TokenId:    token.Id.String(),
	})
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// TestQueryQuoteForwardingFeeRoutesCustomHook asserts the quote is taken against the
// hook the forward will actually use: a custom_hook_id makes the query quote that hook
// (so a relayer routing through a custom IGP learns its real price), while an empty
// custom_hook_id falls back to the mailbox default (zero) hook. Without this, a relayer
// under-quotes to the default hook's fee and MsgForward rejects the forward as
// ErrInsufficientIgpFee.
func TestQueryQuoteForwardingFeeRoutesCustomHook(t *testing.T) {
	ctx := createTestContext()
	bankKeeper := NewMockBankKeeper()
	warpKeeper := NewMockWarpKeeper()
	hyperlaneKeeper := NewMockHyperlaneKeeper()
	hyperlaneKeeper.QuotedFee = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(55)))

	token := createTestHypToken(3, appconsts.BondDenom, warptypes.HYP_TOKEN_TYPE_COLLATERAL)
	warpKeeper.Tokens = append(warpKeeper.Tokens, token)
	warpKeeper.EnrolledRouters[3] = map[uint32]warptypes.RemoteRouter{
		888: {Gas: math.NewInt(123456)},
	}

	queryServer := keeper.NewQueryServerImpl(keeper.NewKeeper(bankKeeper, warpKeeper, hyperlaneKeeper))

	customHook := "0x000000000000000000000000000000000000000000000000000000000000abcd"
	expected, err := util.DecodeHexAddress(customHook)
	require.NoError(t, err)

	// With custom_hook_id: the quote must be taken against the chosen hook.
	resp, err := queryServer.QuoteForwardingFee(ctx, &types.QueryQuoteForwardingFeeRequest{
		DestDomain:   888,
		TokenId:      token.Id.String(),
		CustomHookId: customHook,
	})
	require.NoError(t, err)
	require.Equal(t, "55", resp.Fee.Amount.String())
	require.Equal(t, expected, hyperlaneKeeper.CapturedHook, "quote must route through the custom hook")

	// Without custom_hook_id: the quote falls back to the mailbox default (zero) hook.
	_, err = queryServer.QuoteForwardingFee(ctx, &types.QueryQuoteForwardingFeeRequest{
		DestDomain: 888,
		TokenId:    token.Id.String(),
	})
	require.NoError(t, err)
	require.Equal(t, util.NewZeroAddress(), hyperlaneKeeper.CapturedHook, "empty custom_hook_id must quote the default hook")

	// Invalid custom_hook_id => InvalidArgument.
	_, err = queryServer.QuoteForwardingFee(ctx, &types.QueryQuoteForwardingFeeRequest{
		DestDomain:   888,
		TokenId:      token.Id.String(),
		CustomHookId: "0xnothex",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
