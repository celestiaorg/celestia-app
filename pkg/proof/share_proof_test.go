package proof

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShareProofValidate(t *testing.T) {
	type testCase struct {
		name    string
		sp      ShareProof
		root    []byte
		wantErr bool
	}

	testCases := []testCase{
		{
			name:    "empty share proof returns error",
			sp:      ShareProof{},
			root:    shareProofRoot,
			wantErr: true,
		},
		{
			name:    "valid share proof returns no error",
			sp:      validShareProof(),
			root:    shareProofRoot,
			wantErr: false,
		},
		{
			name:    "share proof with mismatched number of share proofs returns error",
			sp:      mismatchedShareProofs(),
			root:    shareProofRoot,
			wantErr: true,
		},
		{
			name:    "share proof with mismatched number of shares returns error",
			sp:      mismatchedShares(),
			root:    shareProofRoot,
			wantErr: true,
		},
		{
			name:    "valid share proof with incorrect root returns error",
			sp:      validShareProof(),
			root:    incorrectRoot,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.sp.Validate(tc.root)
			if tc.wantErr {
				assert.Error(t, got)
				return
			}
			assert.NoError(t, got)
		})
	}
}

// TestShareProof_NoInt32WrapPanic ensures a malformed proof whose per-proof
// `End - Start` spans wrap an int32 accumulator no longer leads to either a
// "valid" Validate or a slice-bounds panic in VerifyProof.
//
// Three spans of 0x40000001 + 0x40000001 + 0x7FFFFFFE = 0x100000000 wrap to
// int32(0). Combined with empty Data, the previous int32-based length check
// passed. VerifyProof then sliced sp.Data[0:0x40000001] and panicked.
func TestShareProof_NoInt32WrapPanic(t *testing.T) {
	sp := ShareProof{
		Data: [][]byte{},
		ShareProofs: []*NMTProof{
			{Start: 0, End: 0x40000001},
			{Start: 0, End: 0x40000001},
			{Start: 0, End: 0x7FFFFFFE},
		},
		RowProof: &RowProof{
			RowRoots: [][]byte{make([]byte, 32), make([]byte, 32), make([]byte, 32)},
			StartRow: 0,
			EndRow:   2,
		},
	}

	// Validate must fail. The exact error message isn't important — the
	// invariant is "no nil return, no panic".
	assert.Error(t, sp.Validate(root))

	// VerifyProof, if called directly, must not panic on the slice bounds.
	assert.NotPanics(t, func() {
		_ = sp.VerifyProof()
	})
}

func mismatchedShareProofs() ShareProof {
	sp := validShareProof()
	sp.ShareProofs = []*NMTProof{}
	return sp
}

func mismatchedShares() ShareProof {
	sp := validShareProof()
	sp.Data = [][]byte{}
	return sp
}

// validShareProof returns a valid ShareProof for two shares in one row of the
// test square. shareProofRoot is the data root it is valid for.
func validShareProof() ShareProof {
	return newTestSquare().shareProof(testNamespace, rowRange{0, 0, 2})
}

var shareProofRoot = newTestSquare().root
