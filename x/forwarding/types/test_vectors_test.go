package types

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

// testVector represents a single test case for address derivation.
type testVector struct {
	Name          string
	DestDomain    uint32
	DestRecipient string // hex, 32 bytes (64 hex chars without 0x prefix)
}

// sdkTestVectors contains the canonical test vectors for cross-platform SDK implementations.
var sdkTestVectors = []testVector{
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
		DestDomain:    4294967295,
		DestRecipient: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	},
	{
		Name:          "optimism",
		DestDomain:    10,
		DestRecipient: "000000000000000000000000abcdef1234567890abcdef1234567890abcdef12",
	},
}

// TestSDKTestVectors provides test vectors with ALL intermediate values for cross-platform
// SDK developers to verify their address derivation implementations match the Go reference.
func TestSDKTestVectors(t *testing.T) {
	t.Log("=== SDK Test Vectors for Forwarding Address Derivation ===")
	t.Log("Algorithm:")
	t.Log("1. destDomainBytes = 32-byte big-endian encoding of destDomain (value at bytes[28:32])")
	t.Log("2. callDigest = sha256(destDomainBytes || destRecipient)")
	t.Log("3. salt = sha256(\"CELESTIA_FORWARD_V1\" || callDigest)")
	t.Log("4. address = sha256(\"forwarding\" || salt)[:20]")

	for _, v := range sdkTestVectors {
		t.Run(v.Name, func(t *testing.T) {
			recipient, err := hex.DecodeString(v.DestRecipient)
			require.NoError(t, err)
			require.Len(t, recipient, 32, "recipient must be 32 bytes")

			// Step 1: Encode destDomain as 32-byte big-endian
			destDomainBytes := make([]byte, 32)
			binary.BigEndian.PutUint32(destDomainBytes[28:], v.DestDomain)

			// Step 2: Compute call digest (sha256)
			callDigestPreimage := append(destDomainBytes, recipient...)
			callDigestArr := sha256.Sum256(callDigestPreimage)
			callDigest := callDigestArr[:]

			// Step 3: Compute salt with version prefix (sha256)
			saltPreimage := append([]byte(ForwardVersionPrefix), callDigest...)
			saltArr := sha256.Sum256(saltPreimage)
			salt := saltArr[:]

			// Step 4: Derive address (sha256)
			addressPreimage := append([]byte(ModuleName), salt...)
			hash := sha256.Sum256(addressPreimage)
			derivedAddr := hash[:20]

			actualAddr := DeriveForwardingAddress(v.DestDomain, recipient)
			require.Equal(t, derivedAddr, actualAddr.Bytes(), "manual derivation should match function")

			// Log intermediate values for SDK developers
			t.Logf("Vector: %s", v.Name)
			t.Logf("  destDomain: %d (0x%08x)", v.DestDomain, v.DestDomain)
			t.Logf("  destRecipient: 0x%s", v.DestRecipient)
			t.Logf("  destDomainBytes: 0x%s", hex.EncodeToString(destDomainBytes))
			t.Logf("  callDigest: 0x%s", hex.EncodeToString(callDigest))
			t.Logf("  salt: 0x%s", hex.EncodeToString(salt))
			t.Logf("  address: 0x%s", hex.EncodeToString(derivedAddr))
			t.Logf("  bech32: %s", actualAddr.String())
		})
	}
}

// TestDeriveForwardingAddress_Deterministic verifies the address derivation is deterministic.
func TestDeriveForwardingAddress_Deterministic(t *testing.T) {
	destDomain := uint32(1337)
	destRecipient, _ := hex.DecodeString("000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	addr1 := DeriveForwardingAddress(destDomain, destRecipient)
	addr2 := DeriveForwardingAddress(destDomain, destRecipient)
	addr3 := DeriveForwardingAddress(destDomain, destRecipient)

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
	zeroRecipient := make([]byte, 32)
	addr := DeriveForwardingAddress(0, zeroRecipient)
	require.NotNil(t, addr)
	require.Len(t, addr.Bytes(), 20)

	maxDomain := uint32(4294967295)
	addr = DeriveForwardingAddress(maxDomain, zeroRecipient)
	require.NotNil(t, addr)
	require.Len(t, addr.Bytes(), 20)

	maxRecipient, _ := hex.DecodeString("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	addr = DeriveForwardingAddress(maxDomain, maxRecipient)
	require.NotNil(t, addr)
	require.Len(t, addr.Bytes(), 20)
}

// TestSDKTestVectors_JSON outputs the test vectors in JSON format for SDK test files.
func TestSDKTestVectors_JSON(t *testing.T) {
	t.Log("// JSON format for SDK test files")
	t.Log("[")
	for i, v := range sdkTestVectors {
		recipient, _ := hex.DecodeString(v.DestRecipient)
		addr := DeriveForwardingAddress(v.DestDomain, recipient)

		comma := ","
		if i == len(sdkTestVectors)-1 {
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
