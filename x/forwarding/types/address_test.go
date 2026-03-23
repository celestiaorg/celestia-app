package types_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/x/forwarding/types"
	"github.com/cosmos/cosmos-sdk/types/address"
	"github.com/stretchr/testify/require"
)

func TestDeriveForwardingAddress(t *testing.T) {
	testCases := []struct {
		name          string
		destDomain    uint32
		destRecipient []byte
		tokenID       []byte
	}{
		{
			name:          "zero domain and recipient",
			destDomain:    0,
			destRecipient: make([]byte, 32),
			tokenID:       tokenIDBytes(t, 1),
		},
		{
			name:          "typical ethereum domain",
			destDomain:    1,
			destRecipient: hexToBytes(t, "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
			tokenID:       tokenIDBytes(t, 2),
		},
		{
			name:          "arbitrum domain",
			destDomain:    42161,
			destRecipient: hexToBytes(t, "0000000000000000000000001234567890abcdef1234567890abcdef12345678"),
			tokenID:       tokenIDBytes(t, 3),
		},
		{
			name:          "max uint32 domain",
			destDomain:    ^uint32(0), // 4294967295
			destRecipient: bytes.Repeat([]byte{0xff}, 32),
			tokenID:       tokenIDBytes(t, 4),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			addr, err := types.DeriveForwardingAddress(tc.destDomain, tc.destRecipient, tc.tokenID)
			require.NoError(t, err)

			// Verify address is CosmosAddressLen bytes
			require.Len(t, addr, types.CosmosAddressLen, "derived address should be CosmosAddressLen bytes")

			// Verify determinism - same inputs produce same output
			addr2, err := types.DeriveForwardingAddress(tc.destDomain, tc.destRecipient, tc.tokenID)
			require.NoError(t, err)
			require.Equal(t, addr, addr2, "derivation should be deterministic")
		})
	}
}

// TestDeriveForwardingAddressUniqueness verifies that different inputs produce different addresses
func TestDeriveForwardingAddressUniqueness(t *testing.T) {
	baseRecipient := hexToBytes(t, "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	token1 := tokenIDBytes(t, 1)
	token2 := tokenIDBytes(t, 2)

	// Different domains should produce different addresses
	addr1, err := types.DeriveForwardingAddress(1, baseRecipient, token1)
	require.NoError(t, err)
	addr2, err := types.DeriveForwardingAddress(2, baseRecipient, token1)
	require.NoError(t, err)
	require.NotEqual(t, addr1, addr2, "different domains should produce different addresses")

	// Different recipients should produce different addresses
	recipient1 := hexToBytes(t, "0000000000000000000000001111111111111111111111111111111111111111")
	recipient2 := hexToBytes(t, "0000000000000000000000002222222222222222222222222222222222222222")
	addr3, err := types.DeriveForwardingAddress(1, recipient1, token1)
	require.NoError(t, err)
	addr4, err := types.DeriveForwardingAddress(1, recipient2, token1)
	require.NoError(t, err)
	require.NotEqual(t, addr3, addr4, "different recipients should produce different addresses")

	addr5, err := types.DeriveForwardingAddress(1, baseRecipient, token1)
	require.NoError(t, err)
	addr6, err := types.DeriveForwardingAddress(1, baseRecipient, token2)
	require.NoError(t, err)
	require.NotEqual(t, addr5, addr6, "different token IDs should produce different addresses")
}

// TestDeriveForwardingAddressIntermediates verifies the intermediate values in derivation.
// This test is crucial for SDK cross-verification.
// NOTE: This test intentionally re-implements the algorithm to verify the main function
// against an independent implementation. This is NOT code duplication - it ensures the
// function matches the documented algorithm.
func TestDeriveForwardingAddressIntermediates(t *testing.T) {
	destDomain := uint32(1)
	destRecipient := hexToBytes(t, "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	tokenID := tokenIDBytes(t, 1)

	// Step 1: Compute call digest preimage
	destDomainBytes := make([]byte, types.DomainEncodingSize)
	binary.BigEndian.PutUint32(destDomainBytes[types.DomainOffset:], destDomain)
	callDigestPreimage := make([]byte, types.DomainEncodingSize+types.RecipientLength+len(tokenID))
	copy(callDigestPreimage, destDomainBytes)
	copy(callDigestPreimage[types.DomainEncodingSize:], destRecipient)
	copy(callDigestPreimage[types.DomainEncodingSize+types.RecipientLength:], tokenID)

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
	addr, err := types.DeriveForwardingAddress(destDomain, destRecipient, tokenID)
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
		tokenID         string // hex encoded, 32 bytes
		expectedAddress string // hex encoded, 20 bytes
	}{
		{
			name:            "vector_1_ethereum_mainnet",
			destDomain:      1,
			destRecipient:   "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
			tokenID:         "726f757465725f61707000000000000000000000000000010000000000000000",
			expectedAddress: "c2235073e21d77e3b1cc45358a2573d1523bf3fd",
		},
		{
			name:            "vector_2_arbitrum",
			destDomain:      42161,
			destRecipient:   "0000000000000000000000001234567890abcdef1234567890abcdef12345678",
			tokenID:         "726f757465725f61707000000000000000000000000000010000000000000001",
			expectedAddress: "31da1fdcdeae1b347011819b746dc7d862f84454",
		},
		{
			name:            "vector_3_zero_values",
			destDomain:      0,
			destRecipient:   "0000000000000000000000000000000000000000000000000000000000000000",
			tokenID:         "726f757465725f61707000000000000000000000000000010000000000000002",
			expectedAddress: "fe456b8ffdd21578d5f19d68dd6043f2120edf27",
		},
	}

	for _, tc := range testVectors {
		t.Run(tc.name, func(t *testing.T) {
			recipient := hexToBytes(t, tc.destRecipient)
			tokenID := hexToBytes(t, tc.tokenID)
			addr, err := types.DeriveForwardingAddress(tc.destDomain, recipient, tokenID)
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
			_, err := types.DeriveForwardingAddress(1, tc.destRecipient, tokenIDBytes(t, 1))
			require.Error(t, err, "should return error for recipient length %d", len(tc.destRecipient))
			require.ErrorIs(t, err, types.ErrInvalidRecipient)
		})
	}
}

func TestDeriveForwardingAddressReturnsErrorOnInvalidTokenIDLength(t *testing.T) {
	destRecipient := make([]byte, types.RecipientLength)

	testCases := []struct {
		name    string
		tokenID []byte
	}{
		{"empty", []byte{}},
		{"too_short_31_bytes", make([]byte, types.TokenIDLength-1)},
		{"too_long_33_bytes", make([]byte, types.TokenIDLength+1)},
		{"way_too_short", []byte{0x01, 0x02}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := types.DeriveForwardingAddress(1, destRecipient, tc.tokenID)
			require.Error(t, err, "should return error for tokenID length %d", len(tc.tokenID))
			require.ErrorContains(t, err, "invalid token_id length")
		})
	}
}

func tokenIDBytes(t *testing.T, id uint64) []byte {
	t.Helper()
	return hexToBytes(t, fmt.Sprintf("%064x", id))
}

func hexToBytes(t *testing.T, s string) []byte {
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}
