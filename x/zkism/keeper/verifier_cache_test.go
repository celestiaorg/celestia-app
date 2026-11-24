package keeper_test

import (
	"encoding/hex"
	"os"
	"strings"
	"sync"

	"github.com/celestiaorg/celestia-app/v12/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v12/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v12/x/zkism/types"
)

func (suite *KeeperTestSuite) TestVerifierCache() {
	vkey := readGroth16Vkey(suite.T())

	first, err := suite.zkISMKeeper.Verifier(vkey)
	suite.Require().NoError(err)
	second, err := suite.zkISMKeeper.Verifier(append([]byte{}, vkey...))
	suite.Require().NoError(err)
	suite.Require().Same(first, second, "equal key bytes must share one parse")

	for range 2 {
		_, err := suite.zkISMKeeper.Verifier([]byte("invalid"))
		suite.Require().ErrorIs(err, types.ErrInvalidVerifyingKey)
	}
	suite.Require().Equal(1, suite.zkISMKeeper.VerifierCacheLen(), "parse errors must not be cached")
}

// TestVerifierCacheFollowsVerifyingKeyUpdate checks that a proof is verified against the ISM's current key
// after UpdateVerifyingKeys, not a cached parse of the replaced one.
func (suite *KeeperTestSuite) TestVerifierCacheFollowsVerifyingKeyUpdate() {
	proofBz, pubValues := readStateTransitionProofData(suite.T())
	var values types.StateTransitionValues
	suite.Require().NoError(values.Unmarshal(pubValues))

	ism := suite.CreateTestIsm(values.State)
	ism.Owner = testfactory.TestAccAddr
	suite.Require().NoError(suite.zkISMKeeper.SetIsm(suite.ctx, ism.Id, ism))
	_, err := suite.zkISMKeeper.Verifier(ism.Groth16Vkey)
	suite.Require().NoError(err)

	msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
	legacyVkey, err := os.ReadFile("../internal/testdata/groth16_sp1v5_test_key.bin")
	suite.Require().NoError(err)
	_, err = msgServer.UpdateVerifyingKeys(suite.ctx, &types.MsgUpdateVerifyingKeys{
		Id:                  ism.Id,
		Owner:               ism.Owner,
		Groth16Vkey:         legacyVkey,
		StateTransitionVkey: ism.StateTransitionVkey,
		StateMembershipVkey: ism.StateMembershipVkey,
	})
	suite.Require().NoError(err)

	update := &types.MsgUpdateInterchainSecurityModule{Id: ism.Id, Proof: proofBz, PublicValues: pubValues}
	_, err = msgServer.UpdateInterchainSecurityModule(suite.ctx, update)
	suite.Require().ErrorIs(err, types.ErrInvalidProofPrefix)
}

// TestVerifierCacheConcurrentUse checks under -race that goroutines can fill and share the cache, as
// simulation queries do alongside FinalizeBlock.
func (suite *KeeperTestSuite) TestVerifierCacheConcurrentUse() {
	vkey := readGroth16Vkey(suite.T())
	proofBz, pubValues := readStateTransitionProofData(suite.T())
	programVk, err := hex.DecodeString(strings.TrimPrefix(stateVkeyHash, "0x"))
	suite.Require().NoError(err)

	// The first round races misses on a cold cache; the second shares one cached verifier.
	verifyConcurrently := func() []*types.SP1Groth16Verifier {
		verifiers := make([]*types.SP1Groth16Verifier, 8)
		errs := make([]error, len(verifiers))
		var wg sync.WaitGroup
		for i := range verifiers {
			wg.Go(func() {
				verifiers[i], errs[i] = suite.zkISMKeeper.Verifier(vkey)
				if errs[i] == nil {
					errs[i] = verifiers[i].VerifyProof(proofBz, programVk, pubValues)
				}
			})
		}
		wg.Wait()
		for _, err := range errs {
			suite.Require().NoError(err)
		}
		return verifiers
	}

	verifyConcurrently()
	shared, err := suite.zkISMKeeper.Verifier(vkey)
	suite.Require().NoError(err)
	for _, verifier := range verifyConcurrently() {
		suite.Require().Same(shared, verifier)
	}
}
