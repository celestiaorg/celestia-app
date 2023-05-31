package ssz_test

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/celestiaorg/celestia-app/x/ssz/proof"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/tendermint/tendermint/crypto/merkle"
	"github.com/tendermint/tendermint/rpc/client"
)

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
	// TODO get the latest hash from the store and verify it matches the value
	fmt.Printf("SSZ value: %x\n", res.Response.Value)

	// Verifying the proof
	// creating a "proof runtime"
	prt := merkle.DefaultProofRuntime()
	prt.RegisterOpDecoder(storetypes.ProofOpIAVLCommitment, storetypes.CommitmentOpDecoder)
	prt.RegisterOpDecoder(storetypes.ProofOpSimpleMerkleCommitment, storetypes.CommitmentOpDecoder)

	// not entirely sure what this is, but seems potentially insightful
	operators, err := prt.DecodeProof(res.Response.ProofOps)
	require.NoError(t, err)

	fmt.Println("Printing out proof operators")
	for _, op := range operators {
		proof.PrettyPrint(op)
		// fmt.Println()
		// fmt.Println("operation", i, "key", op.GetKey(), op.ProofOp().Type)
		// fmt.Println("data", op.ProofOp().Data)
	}

	// we need the header after the height above due to deferred execution
	height := int64(3)
	blockRes, err := s.cctx.Client.Block(s.cctx.GoContext(), &height)
	require.NoError(t, err)

	// verify the proof to the hash using the insanely abstracted
	// tendermint/ibc/proof code
	root := blockRes.Block.Header.AppHash
	keys := [][]byte{[]byte("ssz"), []byte("hash")}
	value := res.Response.Value

	// This is how we can use the existing cosmos-sdk to verify the proof
	err = prt.VerifyValueFromKeys(
		res.Response.GetProofOps(),
		root,
		keys, value)
	require.NoError(t, err)

	// This is our own implementation of the same thing but greatly simplified
	// and specific to our choice of stores & keys
	branch, err := proof.GenerateProofFromResponse(operators, root, keys, [][]byte{value})
	require.NoError(t, err, "Failed to generate proof of ssz/hash key")
	computedRoot := proof.ComputeRootFromProof(value, branch)
	require.Equal(t, root.Bytes(), computedRoot)

	fmt.Println("Proof verified!")

	// TODO for some reason this test panics at the end with the following errors
	/*
		E[2023-05-31|10:04:45.738] Stopped accept routine, as transport is closed module=p2p numPeers=0
		E[2023-05-31|10:04:45.738] Error serving server                         err="accept tcp 127.0.0.1:49351: use of closed network connection"
		E[2023-05-31|10:04:45.738] Error starting gRPC server                   err="accept tcp 127.0.0.1:49353: use of closed network connection"
	*/
}
