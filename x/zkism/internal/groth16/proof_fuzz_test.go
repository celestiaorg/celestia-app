package groth16_test

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/zkism/internal/groth16"
	curve "github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/stretchr/testify/require"
)

// FuzzUnmarshalProof checks the decoder for the Groth16 proof bytes that
// relayers submit to x/zkism. It must not panic, and every accepted proof must
// be the canonical encoding of its points: the points re-encode, each in the
// compressed or raw form its flag bits selected, to the consumed input bytes.
func FuzzUnmarshalProof(f *testing.F) {
	for _, dir := range []string{"state_transition", "state_membership"} {
		_, proofBytes, _ := readGroth16Testdata(f, dir)
		raw := proofBytes[proofPrefixLen+proofMetadataLen:]
		f.Add(raw)

		proof, err := groth16.UnmarshalProof(raw)
		require.NoError(f, err)
		var compressed bytes.Buffer
		enc := curve.NewEncoder(&compressed)
		require.NoError(f, enc.Encode(&proof.Ar))
		require.NoError(f, enc.Encode(&proof.Bs))
		require.NoError(f, enc.Encode(&proof.Krs))
		f.Add(compressed.Bytes())
	}
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, proofBz []byte) {
		if len(proofBz) > 512 {
			return
		}
		proof, err := groth16.UnmarshalProof(proofBz)
		if err != nil {
			return
		}

		var buf bytes.Buffer
		offset := 0
		encode := func(v any, rawSize, compressedSize int) {
			t.Helper()
			require.Less(t, offset, len(proofBz))
			size, opts := compressedSize, []func(*curve.Encoder){}
			if proofBz[offset]&0xc0 == 0 {
				size, opts = rawSize, append(opts, curve.RawEncoding())
			}
			require.NoError(t, curve.NewEncoder(&buf, opts...).Encode(v))
			offset += size
		}
		encode(&proof.Ar, curve.SizeOfG1AffineUncompressed, curve.SizeOfG1AffineCompressed)
		encode(&proof.Bs, curve.SizeOfG2AffineUncompressed, curve.SizeOfG2AffineCompressed)
		encode(&proof.Krs, curve.SizeOfG1AffineUncompressed, curve.SizeOfG1AffineCompressed)
		require.LessOrEqual(t, offset, len(proofBz))
		require.Equal(t, proofBz[:offset], buf.Bytes(), "accepted proof points are not canonically encoded")
	})
}
