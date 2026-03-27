package keeper_test

import (
	"context"
	"strings"
	"testing"

	"cosmossdk.io/math"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/x/forwarding/keeper"
	"github.com/celestiaorg/celestia-app/v8/x/forwarding/types"
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
