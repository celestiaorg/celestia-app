package keeper_test

import (
	"encoding/hex"
	"testing"

	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	"github.com/stretchr/testify/require"
)

// NOTE: Keeper tests require a non-nil warpKeeper which can't be easily mocked.
// Full keeper tests are in the integration test suite at test/interop/forwarding_integration_test.go

// TestDeriveForwardingAddress verifies address derivation.
// The derivation is done via types.DeriveForwardingAddress, not a keeper method.
func TestDeriveForwardingAddress(t *testing.T) {
	destDomain := uint32(1)
	destRecipient := hexToBytes(t, "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	addr, err := types.DeriveForwardingAddress(destDomain, destRecipient)
	require.NoError(t, err)
	require.NotNil(t, addr)
	require.Len(t, addr, 20)

	// Verify determinism
	addr2, err := types.DeriveForwardingAddress(destDomain, destRecipient)
	require.NoError(t, err)
	require.Equal(t, addr, addr2)
}

func hexToBytes(t *testing.T, s string) []byte {
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}
