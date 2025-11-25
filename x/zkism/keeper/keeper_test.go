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

var (
	newCelestiaHeight  = 31
	celestiaHeaderHash = "70566104cdb660b297eccbe088fd90b5fd30fd325a3f044b8b098544e152ffbd"
	stateVkeyHash      = "0x003317b21c6a8b0731e07e26e90366fb295f00e572e5b6211cad51d699747ea2"
	messageVkeyHash    = "0x00c88cdad907c05533b8755953d58af6a3b753a4e05acc6617d41ca206c25d2a"
	namespaceHex       = "00000000000000000000000000000000000000a8045f161bf468bf4d44"
	publicKeyHex       = "cb01eb93b686fd25866a9f9f13e6a8903a13126e2735e1daf89a35019765d0cc"
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
	suite.zkISMKeeper = testApp.InterchainSecurityModuleKeeper
}

func (suite *KeeperTestSuite) CreateTestIsm(trustedRoot []byte, trustedCelestiaHash []byte, trustedCelestiaHeight uint64) types.InterchainSecurityModule {
	headerHash, err := hex.DecodeString(celestiaHeaderHash)
	suite.Require().NoError(err)

	err = suite.zkISMKeeper.SetHeaderHash(suite.ctx, uint64(newCelestiaHeight), headerHash)
	suite.Require().NoError(err)

	groth16Vkey := readGroth16Vkey(suite.T())

	stateVkeyHex := strings.TrimPrefix(stateVkeyHash, "0x")
	stateVkey, err := hex.DecodeString(stateVkeyHex)
	suite.Require().NoError(err)

	messageVkeyHex := strings.TrimPrefix(messageVkeyHash, "0x")
	messageVkey, err := hex.DecodeString(messageVkeyHex)
	suite.Require().NoError(err)

	ism := types.InterchainSecurityModule{
		Id:                  util.CreateMockHexAddress("ism", 1),
		Groth16Vkey:         groth16Vkey,
		StateTransitionVkey: stateVkey,
		StateMembershipVkey: messageVkey,
		State:               trustedRoot,
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
	ism := suite.CreateTestIsm([]byte("trusted_root"), []byte("trusted_celestia_hash"), 29)

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
			err := suite.zkISMKeeper.SetMessageId(suite.ctx, message.Id().Bytes())
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
					has, err := suite.zkISMKeeper.HasMessageId(suite.ctx, message.Id().Bytes())
					suite.Require().NoError(err)
					suite.Require().False(has, "unexpected message id in store")
				}
			}
		})
	}
}
