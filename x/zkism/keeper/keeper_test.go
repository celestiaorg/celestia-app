package keeper_test

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v6/test/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/proto/tendermint/version"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx         sdk.Context
	zkISMKeeper *keeper.Keeper
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (suite *KeeperTestSuite) SetupTest() {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	suite.ctx = testApp.NewUncachedContext(false, cmtproto.Header{Version: version.Consensus{App: appconsts.Version}})
	suite.zkISMKeeper = testApp.ZKExecutionISMKeeper
}

func randBytes(size uint64) []byte {
	bz := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, bz); err != nil {
		panic(fmt.Errorf("failed to generate random bytes: %w", err))
	}

	return bz
}

func (suite *KeeperTestSuite) TestVerify() {
	var (
		celHeight        = 30
		celHeaderHash    = "2e08a0f992a86551adcb11fe86423e198831739b1b7ce42daefa761d4195b3a3"
		trustedStateRoot = "af50a407e7a9fcba29c46ad31e7690bae4e951e3810e5b898eda29d3d3e92dbe"
		vkeyHash         = "0x00acd6f9c9d0074611353a1e0c94751d3c49beef64ebc3ee82f0ddeadaf242ef"
		namespaceHex     = "00000000000000000000000000000000000000a8045f161bf468bf4d44"
		publicKeyHex     = "c87f6c4cdd4c8ac26cb6a06909e5e252b73043fdf85232c18ae92b9922b65507"
	)

	headerHash, err := hex.DecodeString(celHeaderHash)
	suite.Require().NoError(err)

	err = suite.zkISMKeeper.SetHeaderHash(suite.ctx, uint64(celHeight), headerHash)
	suite.Require().NoError(err)

	vkCommitmentHex := strings.TrimPrefix(vkeyHash, "0x")
	vkCommitment, err := hex.DecodeString(vkCommitmentHex)
	suite.Require().NoError(err)

	trustedRoot, err := hex.DecodeString(trustedStateRoot)
	suite.Require().NoError(err)

	namespace, err := hex.DecodeString(namespaceHex)
	suite.Require().NoError(err)

	pubKey, err := hex.DecodeString(publicKeyHex)
	suite.Require().NoError(err)

	groth16Vk, proofBz, inputsBz := readProofData(suite.T())

	// create an ism with a hardcoded initial trusted state
	ism := types.ZKExecutionISM{
		Id:                  util.CreateMockHexAddress("ism", 1),
		StateTransitionVkey: groth16Vk,
		VkeyCommitment:      vkCommitment,
		StateRoot:           trustedRoot,
		Height:              97,
		Namespace:           namespace,
		SequencerPublicKey:  pubKey,
	}

	err = suite.zkISMKeeper.SetIsm(suite.ctx, ism.Id, ism)
	suite.Require().NoError(err)

	metadata := encodeMetadata(suite.T(), uint64(celHeight), proofBz, inputsBz)

	verified, err := suite.zkISMKeeper.Verify(suite.ctx, ism.Id, metadata, util.HyperlaneMessage{})
	suite.Require().NoError(err)
	suite.Require().True(verified)

	// retrieve the updated ism state
	ism, err = suite.zkISMKeeper.GetIsm(suite.ctx, ism.Id)
	suite.Require().NoError(err)

	inputs := new(types.PublicValues)
	err = inputs.Unmarshal(inputsBz)
	suite.Require().NoError(err)

	suite.Require().Equal(inputs.NewStateRoot[:], ism.StateRoot)
	suite.Require().Equal(inputs.NewHeight, ism.Height)
}

// encodeMetadata: [proofType][proofSize][proof][pubInputsSize][pubInputs]
// Note: Merkle proofs for state membership are omitted here
func encodeMetadata(t *testing.T, height uint64, proofBz, pubInputs []byte) []byte {
	t.Helper()

	heightBz := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBz, height)

	proofSize := make([]byte, 4)
	binary.BigEndian.PutUint32(proofSize, uint32(len(proofBz)))

	pubInputsSize := make([]byte, 4)
	binary.BigEndian.PutUint32(pubInputsSize, uint32(len(pubInputs)))

	var metadata []byte
	metadata = append(metadata, byte(types.ProofTypeSP1Groth16))
	metadata = append(metadata, heightBz...)
	metadata = append(metadata, proofSize...)
	metadata = append(metadata, proofBz...)
	metadata = append(metadata, pubInputsSize...)
	metadata = append(metadata, pubInputs...)

	return metadata
}

func readProofData(t *testing.T) ([]byte, []byte, []byte) {
	t.Helper()

	groth16Vk, err := os.ReadFile("../internal/testdata/groth16_vk.bin")
	require.NoError(t, err, "failed to read verifier key file")

	proofBz, err := os.ReadFile("../internal/testdata/proof.bin")
	require.NoError(t, err, "failed to read proof file")

	inputsBz, err := os.ReadFile("../internal/testdata/sp1_inputs.bin")
	require.NoError(t, err, "failed to read proof file")

	return groth16Vk, proofBz, inputsBz
}
