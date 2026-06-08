package keeper_test

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v9/app"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v9/test/util"
	"github.com/celestiaorg/celestia-app/v9/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v9/x/zkism/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/proto/tendermint/version"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

var (
	stateVkeyHash     = "0x004ac29c473e811dece0f8dd76c8eda80f886d263efb393ec81f54173e54f160"
	messageVkeyHash   = "0x00982fb21526d096c8bf58eda36b5e293ee9ea0f36df441f6a996a974f8feb63"
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

func (suite *KeeperTestSuite) TestVerify() {
	ism := suite.CreateTestIsm([]byte("trusted_root"))

	message := util.HyperlaneMessage{
		Nonce: uint32(1234),
	}

	testCases := []struct {
		name       string
		ismId      util.HexAddress
		message    util.HyperlaneMessage
		authorized bool
		expError   error
	}{
		{
			name:       "success",
			ismId:      ism.Id,
			message:    message,
			authorized: true,
			expError:   nil,
		},
		{
			name:       "ism not found",
			ismId:      util.HexAddress{},
			message:    message,
			authorized: false,
			expError:   types.ErrIsmNotFound,
		},
		{
			name:       "empty message",
			ismId:      ism.Id,
			message:    util.HyperlaneMessage{},
			authorized: false,
			expError:   nil,
		},
		{
			name:       "message id does not exist",
			ismId:      ism.Id,
			message:    util.HyperlaneMessage{Nonce: uint32(10)},
			authorized: false,
			expError:   nil,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := suite.zkISMKeeper.SetMessageId(suite.ctx, ism.Id, message.Id().Bytes())
			suite.Require().NoError(err)

			authorized, err := suite.zkISMKeeper.Verify(suite.ctx, tc.ismId, nil, tc.message)

			if tc.expError != nil {
				suite.Require().Error(err)
				suite.Require().ErrorIs(err, tc.expError)
			} else {
				suite.Require().Equal(tc.authorized, authorized)
				suite.Require().NoError(err)

				if authorized {
					// assert that the message id has been pruned from the store
					has, err := suite.zkISMKeeper.HasMessageId(suite.ctx, ism.Id, message.Id().Bytes())
					suite.Require().NoError(err)
					suite.Require().False(has, "unexpected message id in store")
				}
			}
		})
	}
}
