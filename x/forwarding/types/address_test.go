package types_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
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
			addr := types.DeriveForwardingAddress(tc.destDomain, tc.destRecipient)

			// Verify address is 20 bytes
			require.Len(t, addr, 20, "derived address should be 20 bytes")

			// Verify determinism - same inputs produce same output
			addr2 := types.DeriveForwardingAddress(tc.destDomain, tc.destRecipient)
			require.Equal(t, addr, addr2, "derivation should be deterministic")

			// Log for debugging and TypeScript SDK cross-verification
			t.Logf("destDomain: %d", tc.destDomain)
			t.Logf("destRecipient: %s", hex.EncodeToString(tc.destRecipient))
			t.Logf("derived address: %s", addr.String())
		})
	}
}

// TestDeriveForwardingAddressUniqueness verifies that different inputs produce different addresses
func TestDeriveForwardingAddressUniqueness(t *testing.T) {
	baseRecipient := hexToBytes(t, "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	// Different domains should produce different addresses
	addr1 := types.DeriveForwardingAddress(1, baseRecipient)
	addr2 := types.DeriveForwardingAddress(2, baseRecipient)
	require.NotEqual(t, addr1, addr2, "different domains should produce different addresses")

	// Different recipients should produce different addresses
	recipient1 := hexToBytes(t, "0000000000000000000000001111111111111111111111111111111111111111")
	recipient2 := hexToBytes(t, "0000000000000000000000002222222222222222222222222222222222222222")
	addr3 := types.DeriveForwardingAddress(1, recipient1)
	addr4 := types.DeriveForwardingAddress(1, recipient2)
	require.NotEqual(t, addr3, addr4, "different recipients should produce different addresses")
}

// TestDeriveForwardingAddressIntermediates verifies the intermediate values in derivation.
// This test is crucial for TypeScript SDK cross-verification.
func TestDeriveForwardingAddressIntermediates(t *testing.T) {
	destDomain := uint32(1)
	destRecipient := hexToBytes(t, "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	// Step 1: Compute call digest preimage
	destDomainBytes := make([]byte, 32)
	binary.BigEndian.PutUint32(destDomainBytes[28:], destDomain)
	callDigestPreimage := append(destDomainBytes, destRecipient...)

	t.Logf("destDomainBytes (32 bytes): %s", hex.EncodeToString(destDomainBytes))
	t.Logf("callDigestPreimage (64 bytes): %s", hex.EncodeToString(callDigestPreimage))

	// Step 2: Compute call digest (sha256)
	callDigestArr := sha256.Sum256(callDigestPreimage)
	callDigest := callDigestArr[:]
	t.Logf("callDigest (sha256): %s", hex.EncodeToString(callDigest))

	// Step 3: Compute salt preimage
	saltPreimage := append([]byte(types.ForwardVersionPrefix), callDigest...)
	t.Logf("saltPreimage: %s", hex.EncodeToString(saltPreimage))

	// Step 4: Compute salt (sha256)
	saltArr := sha256.Sum256(saltPreimage)
	salt := saltArr[:]
	t.Logf("salt (sha256): %s", hex.EncodeToString(salt))

	// Step 5: Compute address preimage
	addressPreimage := append([]byte(types.ModuleName), salt...)
	t.Logf("addressPreimage: %s", hex.EncodeToString(addressPreimage))

	// Step 6: Compute final address (sha256)
	hash := sha256.Sum256(addressPreimage)
	derivedAddr := hash[:20]
	t.Logf("sha256 hash: %s", hex.EncodeToString(hash[:]))
	t.Logf("derived address (first 20 bytes): %s", hex.EncodeToString(derivedAddr))

	// Verify this matches the function output
	addr := types.DeriveForwardingAddress(destDomain, destRecipient)
	require.Equal(t, derivedAddr, []byte(addr), "manual derivation should match function output")
}

// TestDeriveForwardingAddressTestVectors provides fixed test vectors for cross-verification.
// These vectors should be used to verify TypeScript SDK implementation.
func TestDeriveForwardingAddressTestVectors(t *testing.T) {
	testVectors := []struct {
		name          string
		destDomain    uint32
		destRecipient string // hex encoded, 32 bytes
		// Expected address will be logged for SDK verification
	}{
		{
			name:          "vector_1_ethereum_mainnet",
			destDomain:    1,
			destRecipient: "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		},
		{
			name:          "vector_2_arbitrum",
			destDomain:    42161,
			destRecipient: "0000000000000000000000001234567890abcdef1234567890abcdef12345678",
		},
		{
			name:          "vector_3_zero_values",
			destDomain:    0,
			destRecipient: "0000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	t.Log("=== TEST VECTORS FOR TYPESCRIPT SDK ===")
	for _, tc := range testVectors {
		t.Run(tc.name, func(t *testing.T) {
			recipient := hexToBytes(t, tc.destRecipient)
			addr := types.DeriveForwardingAddress(tc.destDomain, recipient)

			t.Logf("Test Vector: %s", tc.name)
			t.Logf("  destDomain: %d", tc.destDomain)
			t.Logf("  destRecipient: 0x%s", tc.destRecipient)
			t.Logf("  derivedAddress: %s", addr.String())
			t.Logf("  derivedAddressHex: %s", hex.EncodeToString(addr))
		})
	}
}

// TestDeriveForwardingAddressPanicsOnInvalidLength verifies panic on invalid recipient length
func TestDeriveForwardingAddressPanicsOnInvalidLength(t *testing.T) {
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
			require.Panics(t, func() {
				types.DeriveForwardingAddress(1, tc.destRecipient)
			}, "should panic for recipient length %d", len(tc.destRecipient))
		})
	}
}

func hexToBytes(t *testing.T, s string) []byte {
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}
