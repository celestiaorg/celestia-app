package types_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"os"
	"runtime"
	"slices"
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/internal/groth16"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	"github.com/consensys/gnark-crypto/ecc"
	bn254fr "github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/stretchr/testify/require"
)

const (
	stateTransitionProgramVkHex = "004ac29c473e811dece0f8dd76c8eda80f886d263efb393ec81f54173e54f160"
	stateMembershipProgramVkHex = "00187b5483ec55367414caf254fe354bf24046ebc62f37ef03fed60321c1d7d1"

	// maxVkeyDecodeAlloc bounds the bytes gnark may allocate while decoding a
	// verifying key that passed ValidateGroth16Vkey. Decoding the golden key
	// allocates a few KiB; an inflated length prefix allocates its full size.
	maxVkeyDecodeAlloc = 1 << 20

	// maxPairingProofDiff is the number of proof bytes, relative to a golden
	// proof, that a mutated proof may differ by and still reach the pairing
	// check. Bigger mutations still run every cheap rejection path. This keeps
	// most executions cheap while exploring malleability of each field.
	maxPairingProofDiff = 8
)

// goldenProof is an SP1 Groth16 proof from internal/testdata that verifies.
type goldenProof struct {
	proof, programVk, values []byte
}

func readZKISMTestdata(tb testing.TB) (vkey []byte, proofs []goldenProof) {
	tb.Helper()
	vkey, err := os.ReadFile("../internal/testdata/groth16_vk.bin")
	require.NoError(tb, err)
	for _, tc := range []struct{ dir, programVkHex string }{
		{"state_transition", stateTransitionProgramVkHex},
		{"state_membership", stateMembershipProgramVkHex},
	} {
		proof, err := os.ReadFile("../internal/testdata/" + tc.dir + "/proof.bin")
		require.NoError(tb, err)
		values, err := os.ReadFile("../internal/testdata/" + tc.dir + "/public_values.bin")
		require.NoError(tb, err)
		programVk, err := hex.DecodeString(tc.programVkHex)
		require.NoError(tb, err)
		proofs = append(proofs, goldenProof{proof: proof, programVk: programVk, values: values})
	}
	return vkey, proofs
}

// FuzzValidateGroth16Vkey checks that every verifying key accepted by
// ValidateGroth16Vkey, which guards MsgCreateInterchainSecurityModule,
// MsgUpdateVerifyingKeys and genesis, decodes in gnark without a panic or an
// allocation proportional to an attacker-chosen length prefix.
func FuzzValidateGroth16Vkey(f *testing.F) {
	vkey, _ := readZKISMTestdata(f)
	f.Add(vkey)
	inflated := bytes.Clone(vkey)
	binary.BigEndian.PutUint32(inflated[288:], 1<<30)
	f.Add(inflated)
	uncompressed := bytes.Clone(vkey)
	uncompressed[0] &^= 0xc0
	f.Add(uncompressed)
	f.Add(vkey[:100])

	f.Fuzz(func(t *testing.T, vkey []byte) {
		if len(vkey) > 2*types.Groth16VkeySize {
			return
		}
		if err := types.ValidateGroth16Vkey(vkey); err != nil {
			return
		}
		require.Len(t, vkey, types.Groth16VkeySize)

		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		vk, err := groth16.NewVerifyingKey(vkey)
		runtime.ReadMemStats(&after)
		require.LessOrEqual(t, after.TotalAlloc-before.TotalAlloc, uint64(maxVkeyDecodeAlloc), "decoding an accepted vkey allocated too much")
		if err != nil {
			return
		}
		require.Equal(t, types.Groth16VkeyG1KLength-1, vk.NbPublicWitness())

		var buf bytes.Buffer
		_, err = vk.WriteTo(&buf)
		require.NoError(t, err)
		require.Equal(t, vkey, buf.Bytes(), "accepted vkey must have a canonical encoding")
	})
}

// FuzzPublicValuesUnmarshal checks the decoders for proof public values that
// relayers submit with MsgUpdateInterchainSecurityModule and
// MsgSubmitMessages: no panic, bounded state sizes, and the accepted bytes
// re-encode to the consumed input.
func FuzzPublicValuesUnmarshal(f *testing.F) {
	for _, dir := range []string{"state_transition", "state_membership"} {
		values, err := os.ReadFile("../internal/testdata/" + dir + "/public_values.bin")
		require.NoError(f, err)
		f.Add(values)
	}
	f.Add([]byte{})
	f.Add(binary.LittleEndian.AppendUint64(nil, 1<<62))
	// State lengths on and just outside both bounds.
	for _, lengths := range [][2]int{
		{types.MinStateBytes, types.MaxStateBytes},
		{types.MinStateBytes - 1, types.MinStateBytes},
		{types.MinStateBytes, types.MinStateBytes - 1},
		{types.MaxStateBytes + 1, types.MinStateBytes},
		{types.MinStateBytes, types.MaxStateBytes + 1},
	} {
		var bz []byte
		for _, l := range lengths {
			bz = binary.LittleEndian.AppendUint64(bz, uint64(l))
			bz = append(bz, bytes.Repeat([]byte{0xab}, l)...)
		}
		f.Add(bz)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 2*types.MaxStateTransitionValuesBytes {
			return
		}

		var transition types.StateTransitionValues
		if err := transition.Unmarshal(data); err == nil {
			require.GreaterOrEqual(t, len(transition.State), types.MinStateBytes)
			require.LessOrEqual(t, len(transition.State), types.MaxStateBytes)
			require.GreaterOrEqual(t, len(transition.NewState), types.MinStateBytes)
			require.LessOrEqual(t, len(transition.NewState), types.MaxStateBytes)

			// Unmarshal ignores bytes after NewState, so the encoding must match
			// the consumed prefix rather than all of data.
			encoded, err := transition.Marshal()
			require.NoError(t, err)
			require.True(t, bytes.HasPrefix(data, encoded), "state transition values do not round trip")
			var again types.StateTransitionValues
			require.NoError(t, again.Unmarshal(encoded))
			require.Equal(t, transition, again)
		}

		var membership types.StateMembershipValues
		if err := membership.Unmarshal(data); err == nil {
			encoded, err := membership.Marshal()
			require.NoError(t, err)
			require.Equal(t, data, encoded, "state membership values do not round trip")
		}
	})
}

// FuzzParseMerkleProofMetadata checks the relayer-supplied message metadata
// decoder: no panic, and accepted metadata re-encodes to the input.
func FuzzParseMerkleProofMetadata(f *testing.F) {
	valid := make([]byte, types.MerkleProofMetadataBytes)
	for i := range valid {
		valid[i] = byte(i)
	}
	f.Add(valid)
	f.Add(valid[:types.MerkleProofMetadataBytes-1])
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 2*types.MerkleProofMetadataBytes {
			return
		}
		metadata, err := types.ParseMerkleProofMetadata(data)
		if err != nil {
			return
		}
		encoded := binary.BigEndian.AppendUint32(nil, metadata.Index)
		for i := range util.TreeDepth {
			encoded = append(encoded, metadata.Branch[i][:]...)
		}
		require.Equal(t, data, encoded)
	})
}

// proofStatement is what an SP1 Groth16 proof attests to: the decoded proof
// points and the public inputs, reduced into the BN254 scalar field the same
// way the verifier reduces them.
type proofStatement struct {
	points                        []byte
	programVk, values, proofNonce bn254fr.Element
}

func statementOf(tb testing.TB, proofBz, programVk, values []byte) (proofStatement, bool) {
	tb.Helper()
	if len(proofBz) != types.ProofBytesLen {
		return proofStatement{}, false
	}
	proof, err := groth16.UnmarshalProof(proofBz[types.PrefixLen+types.MetadataLen:])
	if err != nil {
		return proofStatement{}, false
	}
	var buf bytes.Buffer
	_, err = proof.WriteRawTo(&buf)
	require.NoError(tb, err)
	return proofStatement{
		points:     buf.Bytes(),
		programVk:  *groth16.NewBN254FrElement(new(big.Int).SetBytes(programVk)),
		values:     *groth16.NewBN254FrElement(groth16.HashBN254(values)),
		proofNonce: *groth16.NewBN254FrElement(new(big.Int).SetBytes(proofBz[types.PrefixLen+64 : types.PrefixLen+96])),
	}, true
}

func (s proofStatement) equal(o proofStatement) bool {
	return bytes.Equal(s.points, o.points) && s.programVk.Equal(&o.programVk) &&
		s.values.Equal(&o.values) && s.proofNonce.Equal(&o.proofNonce)
}

// FuzzSP1Groth16VerifyProof feeds relayer-controlled proof bytes, program
// verifying keys and public values to SP1Groth16Verifier.VerifyProof. It must
// never panic, and only a statement equal to one of the golden proofs may
// verify: a mutation that changes the proof points or any public input must be
// rejected.
func FuzzSP1Groth16VerifyProof(f *testing.F) {
	vkey, golden := readZKISMTestdata(f)
	verifier, err := types.NewSP1Groth16Verifier(vkey)
	require.NoError(f, err)

	goldenStatements := make([]proofStatement, len(golden))
	for i, g := range golden {
		require.NoError(f, verifier.VerifyProof(g.proof, g.programVk, g.values))
		var ok bool
		goldenStatements[i], ok = statementOf(f, g.proof, g.programVk, g.values)
		require.True(f, ok)
		f.Add(g.proof, g.programVk, g.values)
		f.Add(g.proof, g.programVk, append(bytes.Clone(g.values), 0))
		f.Add(g.proof, g.programVk, []byte{})
		f.Add(g.proof, []byte{}, g.values)
		f.Add(g.proof[:types.ProofBytesLen-1], g.programVk, g.values)
	}
	// The verifier reduces the program key modulo the scalar field order, so
	// this is the same statement as the first golden proof.
	reduced := new(big.Int).Add(new(big.Int).SetBytes(golden[0].programVk), ecc.BN254.ScalarField())
	f.Add(golden[0].proof, reduced.Bytes(), golden[0].values)

	f.Fuzz(func(t *testing.T, proofBz, programVk, values []byte) {
		if len(proofBz) > 2*types.ProofBytesLen || len(programVk) > 64 || len(values) > types.MaxStateTransitionValuesBytes {
			return
		}
		if len(proofBz) == types.ProofBytesLen && minProofDiff(proofBz, golden) > maxPairingProofDiff {
			// Reaching the pairing needs valid curve points and the golden
			// metadata, which large mutations almost never keep; decode the
			// points without the pairing to keep executions cheap.
			_, _ = groth16.UnmarshalProof(proofBz[types.PrefixLen+types.MetadataLen:])
			return
		}

		if err := verifier.VerifyProof(proofBz, programVk, values); err != nil {
			return
		}
		statement, ok := statementOf(t, proofBz, programVk, values)
		require.True(t, ok, "a proof that verified must decode")
		require.True(t, slices.ContainsFunc(goldenStatements, statement.equal),
			"a proof for a statement other than the golden proofs verified")
	})
}

// minProofDiff returns how many bytes proofBz differs by from the closest
// golden proof.
func minProofDiff(proofBz []byte, golden []goldenProof) int {
	best := len(proofBz)
	for _, g := range golden {
		diff := 0
		for i := range proofBz {
			if proofBz[i] != g.proof[i] {
				diff++
			}
		}
		best = min(best, diff)
	}
	return best
}
