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
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/proto/tendermint/version"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"golang.org/x/crypto/sha3"
)

var (
	stateVkeyHash     = "0x004ac29c473e811dece0f8dd76c8eda80f886d263efb393ec81f54173e54f160"
	messageVkeyHash   = "0x00187b5483ec55367414caf254fe354bf24046ebc62f37ef03fed60321c1d7d1"
	merkleTreeAddress = "0000000000000000000000006007ce81d2fd7b9b7f22e71ce9896e00d6017ba8"
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
	suite.zkISMKeeper = testApp.IsmKeeper
}

func (suite *KeeperTestSuite) CreateTestIsm(trustedState []byte) types.InterchainSecurityModule {
	groth16Vkey := readGroth16Vkey(suite.T())

	stateVkeyHex := strings.TrimPrefix(stateVkeyHash, "0x")
	stateVkey, err := hex.DecodeString(stateVkeyHex)
	suite.Require().NoError(err)

	messageVkeyHex := strings.TrimPrefix(messageVkeyHash, "0x")
	messageVkey, err := hex.DecodeString(messageVkeyHex)
	suite.Require().NoError(err)

	merkleTreeAddr, err := hex.DecodeString(merkleTreeAddress)
	suite.Require().NoError(err)

	ism := types.InterchainSecurityModule{
		Id:                  util.CreateMockHexAddress("ism", 1),
		Groth16Vkey:         groth16Vkey,
		StateTransitionVkey: stateVkey,
		StateMembershipVkey: messageVkey,
		State:               trustedState,
		MerkleTreeAddress:   merkleTreeAddr,
	}

	err = suite.zkISMKeeper.SetIsm(suite.ctx, ism.Id, ism)
	suite.Require().NoError(err)

	return ism
}

func randBytes(size uint64) []byte {
	bz := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, bz); err != nil {
		panic(fmt.Errorf("failed to generate random bytes: %w", err))
	}

	return bz
}

func readGroth16Vkey(t *testing.T) []byte {
	t.Helper()

	groth16Vkey, err := os.ReadFile("../internal/testdata/groth16_vk.bin")
	require.NoError(t, err, "failed to read verifier key file")

	return groth16Vkey
}

func readStateTransitionProofData(t *testing.T) ([]byte, []byte) {
	t.Helper()

	proofBz, err := os.ReadFile("../internal/testdata/state_transition/proof.bin")
	require.NoError(t, err, "failed to read proof file")

	inputsBz, err := os.ReadFile("../internal/testdata/state_transition/public_values.bin")
	require.NoError(t, err, "failed to read proof file")

	return proofBz, inputsBz
}

func readStateMembershipProofData(t *testing.T) ([]byte, []byte) {
	t.Helper()

	proofBz, err := os.ReadFile("../internal/testdata/state_membership/proof.bin")
	require.NoError(t, err, "failed to read proof file")

	inputsBz, err := os.ReadFile("../internal/testdata/state_membership/public_values.bin")
	require.NoError(t, err, "failed to read proof file")

	return proofBz, inputsBz
}

// readStateMembershipMessages reads the hyperlane messages, in dispatch order, that the state membership fixtures prove.
func readStateMembershipMessages(t *testing.T) []util.HyperlaneMessage {
	t.Helper()

	bz, err := os.ReadFile("../internal/testdata/state_membership/messages.hex")
	require.NoError(t, err, "failed to read messages file")

	lines := strings.Fields(string(bz))
	messages := make([]util.HyperlaneMessage, 0, len(lines))
	for _, line := range lines {
		raw, err := hex.DecodeString(line)
		require.NoError(t, err)
		message, err := util.ParseHyperlaneMessage(raw)
		require.NoError(t, err)
		messages = append(messages, message)
	}

	return messages
}

// merkleProofMetadata encodes the ISM metadata proving leaves[index] in a hyperlane merkle tree containing leaves.
func merkleProofMetadata(leaves [][32]byte, index uint32) []byte {
	bz := binary.BigEndian.AppendUint32(nil, index)
	for _, node := range merkleBranch(leaves, int(index)) {
		bz = append(bz, node[:]...)
	}
	return bz
}

func (suite *KeeperTestSuite) TestVerify() {
	ism := suite.CreateTestIsm([]byte("trusted_root"))

	// Six leaves are inserted but only five are proven, so leaf 5 is in the tree but beyond the proven count.
	leaves := make([][32]byte, 6)
	tree := util.NewTree(util.ZeroHashes, 0)
	for i := range leaves {
		leaves[i] = util.HyperlaneMessage{Nonce: uint32(i)}.Id()
		suite.Require().NoError(tree.Insert(leaves[i]))
	}
	root := tree.GetRoot()

	treeState := types.MerkleTreeState{Root: root[:], Count: 5}
	metadata := func(index uint32) []byte {
		return merkleProofMetadata(leaves, index)
	}

	testCases := []struct {
		name         string
		ismId        util.HexAddress
		message      util.HyperlaneMessage
		metadata     []byte
		setTreeState bool
		authorized   bool
		expError     error
	}{
		{
			name:         "success: first leaf",
			ismId:        ism.Id,
			message:      util.HyperlaneMessage{Nonce: 0},
			metadata:     metadata(0),
			setTreeState: true,
			authorized:   true,
		},
		{
			name:         "success: last proven leaf",
			ismId:        ism.Id,
			message:      util.HyperlaneMessage{Nonce: 4},
			metadata:     metadata(4),
			setTreeState: true,
			authorized:   true,
		},
		{
			name:         "ism not found",
			ismId:        util.HexAddress{},
			message:      util.HyperlaneMessage{Nonce: 0},
			metadata:     metadata(0),
			setTreeState: true,
			expError:     types.ErrIsmNotFound,
		},
		{
			name:         "empty metadata",
			ismId:        ism.Id,
			message:      util.HyperlaneMessage{Nonce: 0},
			metadata:     nil,
			setTreeState: true,
			expError:     types.ErrInvalidMetadata,
		},
		{
			name:         "truncated metadata",
			ismId:        ism.Id,
			message:      util.HyperlaneMessage{Nonce: 0},
			metadata:     metadata(0)[:types.MerkleProofMetadataBytes-1],
			setTreeState: true,
			expError:     types.ErrInvalidMetadata,
		},
		{
			name:         "no merkle tree state",
			ismId:        ism.Id,
			message:      util.HyperlaneMessage{Nonce: 0},
			metadata:     metadata(0),
			setTreeState: false,
			authorized:   false,
		},
		{
			name:         "message not in tree",
			ismId:        ism.Id,
			message:      util.HyperlaneMessage{Nonce: 100},
			metadata:     metadata(0),
			setTreeState: true,
			authorized:   false,
		},
		{
			name:         "proof for a different leaf",
			ismId:        ism.Id,
			message:      util.HyperlaneMessage{Nonce: 1},
			metadata:     metadata(2),
			setTreeState: true,
			authorized:   false,
		},
		{
			name:         "leaf index beyond proven count",
			ismId:        ism.Id,
			message:      util.HyperlaneMessage{Nonce: 5},
			metadata:     metadata(5),
			setTreeState: true,
			authorized:   false,
		},
		{
			name:    "tampered branch",
			ismId:   ism.Id,
			message: util.HyperlaneMessage{Nonce: 0},
			metadata: func() []byte {
				bz := metadata(0)
				bz[len(bz)-1] ^= 1
				return bz
			}(),
			setTreeState: true,
			authorized:   false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			ism = suite.CreateTestIsm([]byte("trusted_root"))
			if tc.setTreeState {
				suite.Require().NoError(suite.zkISMKeeper.SetMerkleTreeState(suite.ctx, ism.Id, treeState))
			}

			authorized, err := suite.zkISMKeeper.Verify(suite.ctx, tc.ismId, tc.metadata, tc.message)

			if tc.expError != nil {
				suite.Require().ErrorIs(err, tc.expError)
				suite.Require().False(authorized)
			} else {
				suite.Require().NoError(err)
				suite.Require().Equal(tc.authorized, authorized)
			}
		})
	}
}

// merkleBranch returns the merkle branch of leaves[index] in a hyperlane merkle tree containing leaves.
func merkleBranch(leaves [][32]byte, index int) [util.TreeDepth][32]byte {
	var branch [util.TreeDepth][32]byte
	level := leaves
	for depth := range util.TreeDepth {
		if sibling := index ^ 1; sibling < len(level) {
			branch[depth] = level[sibling]
		} else {
			branch[depth] = util.ZeroHashes[depth]
		}
		level = nextMerkleLevel(level, depth)
		index /= 2
	}
	return branch
}

func nextMerkleLevel(level [][32]byte, depth int) [][32]byte {
	next := make([][32]byte, (len(level)+1)/2)
	for i := range next {
		left, right := level[2*i], util.ZeroHashes[depth]
		if 2*i+1 < len(level) {
			right = level[2*i+1]
		}
		h := sha3.NewLegacyKeccak256()
		h.Write(left[:])
		h.Write(right[:])
		copy(next[i][:], h.Sum(nil))
	}
	return next
}
