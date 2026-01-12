package types

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// TestSDKTestVectors provides test vectors with ALL intermediate values for cross-platform
// SDK developers to verify their address derivation implementations match the Go reference.
//
// These vectors should be used by TypeScript, Rust, and other SDK implementations to ensure
// consistent address derivation across all platforms.
func TestSDKTestVectors(t *testing.T) {
	vectors := []struct {
		Name          string
		DestDomain    uint32
		DestRecipient string // hex, 32 bytes (64 hex chars without 0x prefix)
		// Expected intermediate values (for debugging SDK implementations)
		ExpectedDestDomainBytes string // 32-byte big-endian encoding
		ExpectedCallDigest      string // keccak256(destDomainBytes || destRecipient)
		ExpectedSalt            string // keccak256("CELESTIA_FORWARD_V1" || callDigest)
		ExpectedAddress         string // bech32 address
	}{
		{
			Name:          "ethereum_mainnet",
			DestDomain:    1,
			DestRecipient: "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		},
		{
			Name:          "arbitrum",
			DestDomain:    42161,
			DestRecipient: "0000000000000000000000001234567890abcdef1234567890abcdef12345678",
		},
		{
			Name:          "zero_values",
			DestDomain:    0,
			DestRecipient: "0000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			Name:          "max_domain",
			DestDomain:    4294967295, // 0xFFFFFFFF
			DestRecipient: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		},
		{
			Name:          "optimism",
			DestDomain:    10,
			DestRecipient: "000000000000000000000000abcdef1234567890abcdef1234567890abcdef12",
		},
	}

	t.Log("=== SDK Test Vectors for Forwarding Address Derivation ===")
	t.Log("")
	t.Log("Algorithm:")
	t.Log("1. destDomainBytes = 32-byte big-endian encoding of destDomain (value at bytes[28:32])")
	t.Log("2. callDigest = keccak256(destDomainBytes || destRecipient)")
	t.Log("3. salt = keccak256(\"CELESTIA_FORWARD_V1\" || callDigest)")
	t.Log("4. address = sha256(\"forwarding\" || salt)[:20]")
	t.Log("")

	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			// Parse recipient
			recipient, err := hex.DecodeString(v.DestRecipient)
			require.NoError(t, err)
			require.Len(t, recipient, 32, "recipient must be 32 bytes")

			// Step 1: Encode destDomain as 32-byte big-endian
			destDomainBytes := make([]byte, 32)
			binary.BigEndian.PutUint32(destDomainBytes[28:], v.DestDomain)

			// Step 2: Compute call digest
			callDigestPreimage := append(destDomainBytes, recipient...)
			callDigest := crypto.Keccak256(callDigestPreimage)

			// Step 3: Compute salt with version prefix
			saltPreimage := append([]byte(ForwardVersionPrefix), callDigest...)
			salt := crypto.Keccak256(saltPreimage)

			// Step 4: Derive address
			addressPreimage := append([]byte(ModuleName), salt...)
			hash := sha256.Sum256(addressPreimage)
			derivedAddr := hash[:20]

			// Use the actual function to get bech32 address
			actualAddr := DeriveForwardingAddress(v.DestDomain, recipient)

			// Verify our manual computation matches
			require.Equal(t, derivedAddr, actualAddr.Bytes(), "manual derivation should match function")

			// Log ALL intermediate values for SDK developers
			t.Log("---")
			t.Logf("Vector: %s", v.Name)
			t.Logf("  Input:")
			t.Logf("    destDomain: %d (0x%08x)", v.DestDomain, v.DestDomain)
			t.Logf("    destRecipient: 0x%s", v.DestRecipient)
			t.Log("")
			t.Logf("  Intermediate values:")
			t.Logf("    destDomainBytes (32 bytes, big-endian):")
			t.Logf("      0x%s", hex.EncodeToString(destDomainBytes))
			t.Log("")
			t.Logf("    callDigestPreimage (64 bytes = destDomainBytes || destRecipient):")
			t.Logf("      0x%s", hex.EncodeToString(callDigestPreimage))
			t.Log("")
			t.Logf("    callDigest = keccak256(callDigestPreimage):")
			t.Logf("      0x%s", hex.EncodeToString(callDigest))
			t.Log("")
			t.Logf("    saltPreimage (\"%s\" || callDigest):", ForwardVersionPrefix)
			t.Logf("      0x%s", hex.EncodeToString(saltPreimage))
			t.Log("")
			t.Logf("    salt = keccak256(saltPreimage):")
			t.Logf("      0x%s", hex.EncodeToString(salt))
			t.Log("")
			t.Logf("    addressPreimage (\"%s\" || salt):", ModuleName)
			t.Logf("      0x%s", hex.EncodeToString(addressPreimage))
			t.Log("")
			t.Logf("    addressHash = sha256(addressPreimage):")
			t.Logf("      0x%s", hex.EncodeToString(hash[:]))
			t.Log("")
			t.Logf("  Output:")
			t.Logf("    address (20 bytes): 0x%s", hex.EncodeToString(derivedAddr))
			t.Logf("    bech32 address: %s", actualAddr.String())
			t.Log("")
		})
	}
}

// TestDeriveForwardingAddress_Deterministic verifies the address derivation is deterministic.
func TestDeriveForwardingAddress_Deterministic(t *testing.T) {
	destDomain := uint32(1337)
	destRecipient, _ := hex.DecodeString("000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	// Derive multiple times
	addr1 := DeriveForwardingAddress(destDomain, destRecipient)
	addr2 := DeriveForwardingAddress(destDomain, destRecipient)
	addr3 := DeriveForwardingAddress(destDomain, destRecipient)

	// All should be identical
	require.Equal(t, addr1, addr2)
	require.Equal(t, addr2, addr3)
}

// TestDeriveForwardingAddress_Uniqueness verifies different inputs produce different addresses.
func TestDeriveForwardingAddress_Uniqueness(t *testing.T) {
	recipient1, _ := hex.DecodeString("000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	recipient2, _ := hex.DecodeString("000000000000000000000000cafebabecafebabecafebabecafebabecafebabe")

	// Different domains, same recipient
	addr1 := DeriveForwardingAddress(1, recipient1)
	addr2 := DeriveForwardingAddress(2, recipient1)
	require.NotEqual(t, addr1, addr2, "different domains should produce different addresses")

	// Same domain, different recipients
	addr3 := DeriveForwardingAddress(1, recipient1)
	addr4 := DeriveForwardingAddress(1, recipient2)
	require.NotEqual(t, addr3, addr4, "different recipients should produce different addresses")

	// Different domains, different recipients
	addr5 := DeriveForwardingAddress(1, recipient1)
	addr6 := DeriveForwardingAddress(2, recipient2)
	require.NotEqual(t, addr5, addr6, "different inputs should produce different addresses")
}

// TestDeriveForwardingAddress_EdgeCases tests boundary conditions.
func TestDeriveForwardingAddress_EdgeCases(t *testing.T) {
	// Zero recipient
	zeroRecipient := make([]byte, 32)
	addr := DeriveForwardingAddress(0, zeroRecipient)
	require.NotNil(t, addr)
	require.Len(t, addr.Bytes(), 20)

	// Max domain
	maxDomain := uint32(4294967295) // 0xFFFFFFFF
	addr = DeriveForwardingAddress(maxDomain, zeroRecipient)
	require.NotNil(t, addr)
	require.Len(t, addr.Bytes(), 20)

	// Max recipient
	maxRecipient, _ := hex.DecodeString("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	addr = DeriveForwardingAddress(maxDomain, maxRecipient)
	require.NotNil(t, addr)
	require.Len(t, addr.Bytes(), 20)
}

// TestSDKTestVectors_JSON outputs the test vectors in a format suitable for SDK test files.
// Run with: go test -v -run TestSDKTestVectors_JSON
func TestSDKTestVectors_JSON(t *testing.T) {
	vectors := []struct {
		DestDomain    uint32
		DestRecipient string
	}{
		{1, "000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"},
		{42161, "0000000000000000000000001234567890abcdef1234567890abcdef12345678"},
		{0, "0000000000000000000000000000000000000000000000000000000000000000"},
		{4294967295, "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
		{10, "000000000000000000000000abcdef1234567890abcdef1234567890abcdef12"},
	}

	t.Log("")
	t.Log("// JSON format for SDK test files")
	t.Log("[")
	for i, v := range vectors {
		recipient, _ := hex.DecodeString(v.DestRecipient)
		addr := DeriveForwardingAddress(v.DestDomain, recipient)

		comma := ","
		if i == len(vectors)-1 {
			comma = ""
		}

		t.Logf("  {")
		t.Logf("    \"destDomain\": %d,", v.DestDomain)
		t.Logf("    \"destRecipient\": \"0x%s\",", v.DestRecipient)
		t.Logf("    \"expectedAddress\": \"%s\"", addr.String())
		t.Logf("  }%s", comma)
	}
	t.Log("]")
}
