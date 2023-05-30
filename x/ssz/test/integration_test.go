package ssz_test

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
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
	err = prt.VerifyValueFromKeys(
		res.Response.GetProofOps(),
		blockRes.Block.Header.AppHash,
		[][]byte{[]byte("ssz"), []byte("hash")}, res.Response.Value)
	require.NoError(t, err)
}
