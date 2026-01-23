package types_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	"github.com/cosmos/cosmos-sdk/types/address"
	"github.com/stretchr/testify/require"
)

func TestDeriveForwardingAddress(t *testing.T) {
	testCases := []struct {
		name          string
		destDomain    uint32
		destRecipient []byte
	}{
		{
			name:          "zero domain and recipient",
			destDomain:    0,
			destRecipient: make([]byte, 32),
		},
		{
			name:          "typical ethereum domain",
			destDomain:    1,
			destRecipient: hexToBytes(t, "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		},
		{
			name:          "arbitrum domain",
			destDomain:    42161,
			destRecipient: hexToBytes(t, "0000000000000000000000001234567890abcdef1234567890abcdef12345678"),
		},
		{
			name:          "max uint32 domain",
			destDomain:    ^uint32(0), // 4294967295
			destRecipient: bytes.Repeat([]byte{0xff}, 32),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			addr, err := types.DeriveForwardingAddress(tc.destDomain, tc.destRecipient)
			require.NoError(t, err)

			// Verify address is CosmosAddressLen bytes
			require.Len(t, addr, types.CosmosAddressLen, "derived address should be CosmosAddressLen bytes")

			// Verify determinism - same inputs produce same output
			addr2, err := types.DeriveForwardingAddress(tc.destDomain, tc.destRecipient)
			require.NoError(t, err)
			require.Equal(t, addr, addr2, "derivation should be deterministic")
		})
	}
}

// TestDeriveForwardingAddressUniqueness verifies that different inputs produce different addresses
func TestDeriveForwardingAddressUniqueness(t *testing.T) {
	baseRecipient := hexToBytes(t, "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	// Different domains should produce different addresses
	addr1, err := types.DeriveForwardingAddress(1, baseRecipient)
	require.NoError(t, err)
	addr2, err := types.DeriveForwardingAddress(2, baseRecipient)
	require.NoError(t, err)
	require.NotEqual(t, addr1, addr2, "different domains should produce different addresses")

	// Different recipients should produce different addresses
	recipient1 := hexToBytes(t, "0000000000000000000000001111111111111111111111111111111111111111")
	recipient2 := hexToBytes(t, "0000000000000000000000002222222222222222222222222222222222222222")
	addr3, err := types.DeriveForwardingAddress(1, recipient1)
	require.NoError(t, err)
	addr4, err := types.DeriveForwardingAddress(1, recipient2)
	require.NoError(t, err)
	require.NotEqual(t, addr3, addr4, "different recipients should produce different addresses")
}

// TestDeriveForwardingAddressIntermediates verifies the intermediate values in derivation.
// This test is crucial for SDK cross-verification.
// NOTE: This test intentionally re-implements the algorithm to verify the main function
// against an independent implementation. This is NOT code duplication - it ensures the
// function matches the documented algorithm.
func TestDeriveForwardingAddressIntermediates(t *testing.T) {
	destDomain := uint32(1)
	destRecipient := hexToBytes(t, "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	// Step 1: Compute call digest preimage
	destDomainBytes := make([]byte, types.DomainEncodingSize)
	binary.BigEndian.PutUint32(destDomainBytes[types.DomainOffset:], destDomain)
	callDigestPreimage := make([]byte, types.DomainEncodingSize+types.RecipientLength)
	copy(callDigestPreimage, destDomainBytes)
	copy(callDigestPreimage[types.DomainEncodingSize:], destRecipient)

	// Step 2: Compute call digest (sha256)
	callDigestArr := sha256.Sum256(callDigestPreimage)
	callDigest := callDigestArr[:]

	// Step 3: Compute salt preimage with version byte
	saltPreimage := make([]byte, 1+32)
	saltPreimage[0] = types.ForwardVersion
	copy(saltPreimage[1:], callDigest)

	// Step 4: Compute salt (sha256)
	saltArr := sha256.Sum256(saltPreimage)
	salt := saltArr[:]

	// Step 5: Use SDK's address.Module for the final derivation
	derivedAddr := address.Module(types.ModuleName, salt)[:types.CosmosAddressLen]

	// Verify this matches the function output
	addr, err := types.DeriveForwardingAddress(destDomain, destRecipient)
	require.NoError(t, err)
	require.Equal(t, derivedAddr, addr, "manual derivation should match function output")
}

// TestDeriveForwardingAddressTestVectors provides fixed test vectors for cross-verification.
// These vectors should be used to verify SDK implementations.
func TestDeriveForwardingAddressTestVectors(t *testing.T) {
	testVectors := []struct {
		name            string
		destDomain      uint32
		destRecipient   string // hex encoded, 32 bytes
		expectedAddress string // hex encoded, 20 bytes
	}{
		{
			name:            "vector_1_ethereum_mainnet",
			destDomain:      1,
			destRecipient:   "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
			expectedAddress: "a5552e3961868e720b55166437869f581ec058c8",
		},
		{
			name:            "vector_2_arbitrum",
			destDomain:      42161,
			destRecipient:   "0000000000000000000000001234567890abcdef1234567890abcdef12345678",
			expectedAddress: "d1643f5d081f9e2a389dca3bc2dfdccfc0dace1b",
		},
		{
			name:            "vector_3_zero_values",
			destDomain:      0,
			destRecipient:   "0000000000000000000000000000000000000000000000000000000000000000",
			expectedAddress: "bf71c5b5b1ba95ab45af03088442b46bdc48e029",
		},
	}

	for _, tc := range testVectors {
		t.Run(tc.name, func(t *testing.T) {
			recipient := hexToBytes(t, tc.destRecipient)
			addr, err := types.DeriveForwardingAddress(tc.destDomain, recipient)
			require.NoError(t, err)

			actualHex := hex.EncodeToString(addr)
			require.Equal(t, tc.expectedAddress, actualHex,
				"address mismatch for %s: expected %s, got %s",
				tc.name, tc.expectedAddress, actualHex)
		})
	}
}

// TestDeriveForwardingAddressReturnsErrorOnInvalidLength verifies error on invalid recipient length
func TestDeriveForwardingAddressReturnsErrorOnInvalidLength(t *testing.T) {
	testCases := []struct {
		name          string
		destRecipient []byte
	}{
		{"empty", []byte{}},
		{"too_short_31_bytes", make([]byte, 31)},
		{"too_long_33_bytes", make([]byte, 33)},
		{"way_too_short", []byte{0x01, 0x02}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := types.DeriveForwardingAddress(1, tc.destRecipient)
			require.Error(t, err, "should return error for recipient length %d", len(tc.destRecipient))
			require.ErrorIs(t, err, types.ErrInvalidRecipient)
		})
	}
}

func hexToBytes(t *testing.T, s string) []byte {
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}
