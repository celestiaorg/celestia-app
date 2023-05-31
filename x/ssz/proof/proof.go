// Utilities for creating inclusion proofs for SSZ state
package proof

import (
	"bytes"
	"errors"
	"fmt"

	ics23 "github.com/confio/ics23/go"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/tendermint/tendermint/crypto/merkle"
)

type BranchNode struct {
	Prefix []byte
	Suffix []byte
}

func EncodeVarintProto(l int) []byte {
	// avoid multiple allocs for normal case
	res := make([]byte, 0, 8)
	for l >= 1<<7 {
		res = append(res, uint8(l&0x7f|0x80))
		l >>= 7
	}
	res = append(res, uint8(l))
	return res
}

func GetHashPrefix() []byte {
	hashLeafPrefix := []byte{0, 2, 4}
	hashInBytes := []byte{104, 97, 115, 104}
	prefix := append(hashLeafPrefix, append(EncodeVarintProto(len(hashInBytes)), hashInBytes...)...)
	return prefix
}

func GetSSZPrefix() []byte {
	sszLeafPrefix := []byte{0}
	sszInBytes := []byte{115, 115, 122}
	prefix := append(sszLeafPrefix, append(EncodeVarintProto(len(sszInBytes)), sszInBytes...)...)
	return prefix
}

func VarIntProto32() []byte {
	return EncodeVarintProto(32)
}

/*
This is copied from VerifyValueFromKeys, which is a method on ProofRuntime
"prt := merkle.DefaultProofRuntime()"
VerifyValueFromKeys ends up as VerifyFromKeys in
crypto/merkle/proof_op.go in celestia-core
Then CommitmentOpDecoder is from store/types/proof.go storetypes here
Which decodes it to an ics23 CommitmentOp
*/
func GenerateProofFromResponse(poz merkle.ProofOperators, root []byte, keys [][]byte, args [][]byte) (branch []BranchNode, err error) {
	branch = make([]BranchNode, 0)
	var computedRoot []byte
	for _, op := range poz {
		decodedOp, errDecoding := storetypes.CommitmentOpDecoder(op.ProofOp())
		if errDecoding != nil {
			fmt.Printf("failed decoding CommitmentOp: %v", errDecoding)
			return nil, errDecoding
		}
		var castDecodedOp storetypes.CommitmentOp
		switch v := decodedOp.(type) {
		case storetypes.CommitmentOp:
			castDecodedOp = v
		default:
			return nil, errors.New("not CommitmentOp %v")
		}
		// PrettyPrint(castDecodedOp)

		// Now given the CommitmentOp, `op.Run(...)` happens in VerifyFromKeys
		// The first step is `op.Proof.Calculate()` to get the root
		// The root is what is eventually returned

		// op.Proof.Calculate() first casts based on the proof type
		p := castDecodedOp.Proof
		var existenceProof *ics23.ExistenceProof
		switch v := p.Proof.(type) {
		case *ics23.CommitmentProof_Exist:
			existenceProof = v.Exist
		default:
			return nil, errors.New("not ExistenceProof")
		}

		// Now that we have an existance proof, let's mimic
		// `func (p *ExistenceProof) calculate(spec *ProofSpec)`
		// For existence proof, first call `calculate(nil)`

		// We only support certain types of existenceProof and an expected spec
		if existenceProof.Leaf.PrehashKey != ics23.HashOp_NO_HASH {
			panic("Prehash key not supported")
		}
		if existenceProof.Leaf.PrehashValue != ics23.HashOp_SHA256 {
			panic("Prehash value not supported")
		}
		if existenceProof.Leaf.Hash != ics23.HashOp_SHA256 {
			panic("Hash not supported")
		}
		if existenceProof.Leaf.Length != ics23.LengthOp_VAR_PROTO {
			panic("LengthOp mode not supported")
		}

		key := existenceProof.Key
		value := existenceProof.Value
		prefix := existenceProof.Leaf.Prefix
		prefix = append(prefix, append(EncodeVarintProto(len(key)), key...)...)
		valueHash := Hash(value)
		prefix = append(prefix, append(EncodeVarintProto(len(valueHash)), valueHash...)...)
		manualResult := Hash(prefix)

		res, err := existenceProof.Leaf.Apply(key, value)
		if err != nil {
			return nil, err
		}

		if !bytes.Equal(manualResult, res) {
			panic("computed leaf application != ground truth leaf application")
		}

		for _, step := range existenceProof.Path {
			preimage := step.Prefix
			preimage = append(preimage, res...)
			preimage = append(preimage, step.Suffix...)
			res = Hash(preimage)
			branch = append(branch, BranchNode{Prefix: step.Prefix, Suffix: step.Suffix})
		}
		computedRoot, err = existenceProof.Calculate()
		if err != nil {
			return nil, err
		}
	}

	// The last computedRoot should equal the ground truth root
	if !bytes.Equal(root, computedRoot) {
		return nil, errors.New("root mismatch")
	}

	return branch, nil

}

func ComputeRootFromProof(leaf []byte, branch []BranchNode) (res []byte) {
	valueHash := Hash(leaf)
	res = Hash(append(GetHashPrefix(), append(VarIntProto32(), valueHash...)...))
	res = Hash(res)
	res = Hash(append(GetSSZPrefix(), append(VarIntProto32(), res...)...))
	for _, step := range branch {
		preimage := step.Prefix
		preimage = append(preimage, res...)
		preimage = append(preimage, step.Suffix...)
		res = Hash(preimage)
	}
	return res
}
