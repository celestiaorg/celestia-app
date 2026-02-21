package keeper_test

import (
	"context"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/x/forwarding/keeper"
	"github.com/celestiaorg/celestia-app/v8/x/forwarding/types"
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
			})
			require.Error(t, err)
			// Invalid hex should return InvalidArgument
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}
