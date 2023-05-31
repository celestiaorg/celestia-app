package ssz_test

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/celestiaorg/celestia-app/x/ssz"
	ics23 "github.com/confio/ics23/go"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/tendermint/tendermint/crypto/merkle"
	"github.com/tendermint/tendermint/rpc/client"
)

type BranchNode struct {
	Prefix []byte
	Suffix []byte
}

func TestStandardSDKIntegrationTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SDK integration test in short mode.")
	}
	suite.Run(t, new(StandardSDKIntegrationTestSuite))
}

type StandardSDKIntegrationTestSuite struct {
	suite.Suite

	cctx testnode.Context
	ecfg encoding.Config
}

func (s *StandardSDKIntegrationTestSuite) SetupSuite() {
	t := s.T()
	t.Log("setting up integration test suite")
	_, cctx := testnode.DefaultNetwork(t)
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	s.cctx = cctx
}

func (s *StandardSDKIntegrationTestSuite) TestHash() {
	t := s.T()
	_, err := s.cctx.WaitForHeight(2)
	require.NoError(t, err)

	res, err := s.cctx.Client.ABCIQueryWithOptions(
		s.cctx.GoContext(),
		"store/ssz/key",
		[]byte("hash"),
		client.ABCIQueryOptions{Height: 2, Prove: true},
	)
	require.NoError(t, err)

	// the bytes in the store that should be the hash of the ssz validator set
	fmt.Println("hash value", res.Response.Value)

	// Verifying the proof
	// creating a "proof runtime"
	prt := merkle.DefaultProofRuntime()
	prt.RegisterOpDecoder(storetypes.ProofOpIAVLCommitment, storetypes.CommitmentOpDecoder)
	prt.RegisterOpDecoder(storetypes.ProofOpSimpleMerkleCommitment, storetypes.CommitmentOpDecoder)

	// not entirely sure what this is, but seems potentially insightful
	operators, err := prt.DecodeProof(res.Response.ProofOps)
	require.NoError(t, err)
	for i, op := range operators {
		fmt.Println()
		fmt.Println("operation", i, "key", op.GetKey(), op.ProofOp().Type)
		fmt.Println("data", op.ProofOp().Data)
	}

	// we need the header after the height above due to deferred execution
	h := int64(3)
	blockRes, err := s.cctx.Client.Block(s.cctx.GoContext(), &h)
	require.NoError(t, err)

	// verify the proof to the hash using the insanely abstracted
	// tendermint/ibc/proof code
	root := blockRes.Block.Header.AppHash
	keys := [][]byte{[]byte("ssz"), []byte("hash")}
	value := res.Response.Value

	fmt.Printf("Keys value %x %x\n", keys[0], keys[1])
	fmt.Printf("SSZ Value %x\n", value)
	fmt.Printf("AppHash %x\n", root)

	branch, _ := VerifyFromKeys(operators, root, keys, [][]byte{value})
	computedRoot := ComputeRootFromProof(value, branch)
	fmt.Printf("Computed Root %x\n", computedRoot)

	err = prt.VerifyValueFromKeys(
		res.Response.GetProofOps(),
		root,
		keys, value)
	require.NoError(t, err)

	require.Equal(t, root.Bytes(), computedRoot)
}

// VerifyValueFromKeys ends up as VerifyFromKeys in
// crypto/merkle/proof_op.go in celestia-core

// Then CommitmentOpDecoder is from store/types/proof.go storetypes here
// Which decodes it to an ics23 CommitmentOp

// VerifyFromKeys performs the same verification logic as the normal Verify
// method, except it does not perform any processing on the keypath. This is
// useful when using keys that have split or escape points as a part of the key.
func VerifyFromKeys(poz merkle.ProofOperators, root []byte, keys [][]byte, args [][]byte) (branch []BranchNode, err error) {
	fmt.Println("Starting args", args)
	branch = make([]BranchNode, 0)
	for _, op := range poz {
		decodedOp, errDecoding := storetypes.CommitmentOpDecoder(op.ProofOp())
		if errDecoding != nil {
			fmt.Printf("failed decoding CommitmentOp: %v", err)
		}
		var castDecodedOp storetypes.CommitmentOp
		switch v := decodedOp.(type) {
		case storetypes.CommitmentOp:
			castDecodedOp = v
		default:
			panic("Not CommitmentOp")
		}
		// PrettyPrint(castDecodedOp)

		// Now given the CommitmentOp, `op.Run(...)` happens
		// The first step is `op.Proof.Calculate()` to get the root
		// The root is what is eventually returned

		// op.Proof.Calculate() first casts based on the proof type
		p := castDecodedOp.Proof
		var existenceProof *ics23.ExistenceProof
		switch v := p.Proof.(type) {
		case *ics23.CommitmentProof_Exist:
			existenceProof = v.Exist
		default:
			panic("Not Existence proof")
		}

		// Now that we have an existance proof, let's mimic
		// `func (p *ExistenceProof) calculate(spec *ProofSpec)`
		// For existance proof, first call `calculate(nil)`
		key := existenceProof.Key
		value := existenceProof.Value
		fmt.Printf("key %x\n", key)
		fmt.Printf("value %x\n", value)

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

		prefix := existenceProof.Leaf.Prefix
		fmt.Println("prefix", prefix)
		fmt.Println("key", key)
		prefix = append(prefix, append(ssz.EncodeVarintProto(len(key)), key...)...)
		valueHash := ssz.Hash(value)
		prefix = append(prefix, append(ssz.EncodeVarintProto(len(valueHash)), valueHash...)...)
		manualResult := ssz.Hash(prefix)
		fmt.Printf("Manual leaf application computation %x\n", manualResult)

		res, err := existenceProof.Leaf.Apply(key, value)
		if err != nil {
			fmt.Println("Error applying leaf", err)
		}
		fmt.Printf("After leaf application %x\n", res)

		// TODO require that res == manualResult

		for _, step := range existenceProof.Path {
			// res = step.Apply(res)
			preimage := step.Prefix
			preimage = append(preimage, res...)
			preimage = append(preimage, step.Suffix...)
			res = ssz.Hash(preimage)
			branch = append(branch, BranchNode{Prefix: step.Prefix, Suffix: step.Suffix})
		}

		fmt.Printf("Manual calculate %x\n", res)
		computedRoot, err := existenceProof.Calculate()
		fmt.Printf("True result %x\n", computedRoot)

	}

	return branch, nil

}

func GetHashPrefix() []byte {
	hashLeafPrefix := []byte{0, 2, 4}
	hashInBytes := []byte{104, 97, 115, 104}
	prefix := append(hashLeafPrefix, append(ssz.EncodeVarintProto(len(hashInBytes)), hashInBytes...)...)
	return prefix
}

func GetSSZPrefix() []byte {
	sszLeafPrefix := []byte{0}
	sszInBytes := []byte{115, 115, 122}
	prefix := append(sszLeafPrefix, append(ssz.EncodeVarintProto(len(sszInBytes)), sszInBytes...)...)
	return prefix
}

func VarIntProto32() []byte {
	return ssz.EncodeVarintProto(32)
}

func ComputeRootFromProof(leaf []byte, branch []BranchNode) (res []byte) {
	valueHash := ssz.Hash(leaf)
	res = ssz.Hash(append(GetHashPrefix(), append(VarIntProto32(), valueHash...)...))
	res = ssz.Hash(res)
	res = ssz.Hash(append(GetSSZPrefix(), append(VarIntProto32(), res...)...))
	for _, step := range branch {
		preimage := step.Prefix
		preimage = append(preimage, res...)
		preimage = append(preimage, step.Suffix...)
		res = ssz.Hash(preimage)
	}
	return res
}
