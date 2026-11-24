package keeper_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"runtime"
	"sync"
	"sync/atomic"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v12/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v12/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v12/x/zkism/keeper"
	"github.com/celestiaorg/celestia-app/v12/x/zkism/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// handlerOutcome is everything a proof handler makes observable.
type handlerOutcome struct {
	res    any
	err    error
	gas    uint64
	events sdk.Events
}

// runProofMsg runs msg on a cache of suite.ctx with a fresh gas meter and event manager.
func (suite *KeeperTestSuite) runProofMsg(msg sdk.Msg) handlerOutcome {
	ctx, _ := suite.ctx.CacheContext()
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).WithEventManager(sdk.NewEventManager())
	msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)

	var out handlerOutcome
	switch msg := msg.(type) {
	case *types.MsgUpdateInterchainSecurityModule:
		out.res, out.err = msgServer.UpdateInterchainSecurityModule(ctx, msg)
	case *types.MsgSubmitMessages:
		out.res, out.err = msgServer.SubmitMessages(ctx, msg)
	default:
		suite.FailNow("unexpected msg type")
	}
	out.gas, out.events = ctx.GasMeter().GasConsumed(), ctx.EventManager().Events()
	return out
}

// TestPreverifyEquivalence checks that a handler behaves the same whether it verifies its proof inline or
// reuses the verdict PreverifyProofs recorded.
func (suite *KeeperTestSuite) TestPreverifyEquivalence() {
	transitionProof, transitionValues := readStateTransitionProofData(suite.T())
	var transition types.StateTransitionValues
	suite.Require().NoError(transition.Unmarshal(transitionValues))
	membershipState, err := hex.DecodeString(stateMembershipTrustedState)
	suite.Require().NoError(err)
	membershipProof, membershipValues := readStateMembershipProofData(suite.T())

	flipPrefix := func(proof []byte) []byte {
		proof = bytes.Clone(proof)
		proof[0] ^= 1
		return proof
	}
	badTreeRoot := func() []byte {
		var values types.StateMembershipValues
		suite.Require().NoError(values.Unmarshal(membershipValues))
		values.TreeRoot[0] ^= 1
		bz, err := values.Marshal()
		suite.Require().NoError(err)
		return bz
	}
	update := func(proof, values []byte) *types.MsgUpdateInterchainSecurityModule {
		return &types.MsgUpdateInterchainSecurityModule{Proof: proof, PublicValues: values}
	}
	submit := func(proof, values []byte) *types.MsgSubmitMessages {
		return &types.MsgSubmitMessages{Proof: proof, PublicValues: values}
	}
	badNewState := marshalStateTransitionValues(suite, transitionValues, func(v *types.StateTransitionValues) { v.NewState[0] ^= 1 })

	testCases := []struct {
		name     string
		state    []byte
		msg      sdk.Msg
		expError error
	}{
		{"update: valid", transition.State, update(transitionProof, transitionValues), nil},
		{"update: proof fails", transition.State, update(transitionProof, badNewState), types.ErrInvalidProof},
		{"update: wrong prefix", transition.State, update(flipPrefix(transitionProof), transitionValues), types.ErrInvalidProofPrefix},
		{"submit: valid", membershipState, submit(membershipProof, membershipValues), nil},
		{"submit: proof fails", membershipState, submit(membershipProof, badTreeRoot()), types.ErrInvalidProof},
		{"submit: wrong prefix", membershipState, submit(flipPrefix(membershipProof), membershipValues), types.ErrInvalidProofPrefix},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			ism := suite.CreateTestIsm(tc.state)
			programVk := ism.StateMembershipVkey
			switch msg := tc.msg.(type) {
			case *types.MsgUpdateInterchainSecurityModule:
				msg.Id, programVk = ism.Id, ism.StateTransitionVkey
			case *types.MsgSubmitMessages:
				msg.Id = ism.Id
			}
			proof, values := proofOf(tc.msg)

			suite.zkISMKeeper.PreverifyProofs(suite.ctx, nil)
			inline := suite.runProofMsg(tc.msg)

			suite.zkISMKeeper.PreverifyProofs(suite.ctx, []sdk.Msg{tc.msg})
			suite.Require().True(suite.zkISMKeeper.HasVerdict(ism.Groth16Vkey, programVk, proof, values))
			reused := suite.runProofMsg(tc.msg)

			if tc.expError == nil {
				suite.Require().NoError(inline.err)
				suite.Require().NoError(reused.err)
			} else {
				suite.Require().ErrorIs(inline.err, tc.expError)
				suite.Require().ErrorIs(reused.err, tc.expError)
				suite.Require().Equal(inline.err.Error(), reused.err.Error())
			}
			suite.Require().Equal(inline.res, reused.res)
			suite.Require().Equal(inline.gas, reused.gas)
			suite.Require().Equal(inline.events, reused.events)
		})
	}
}

func proofOf(msg sdk.Msg) (proof, publicValues []byte) {
	switch msg := msg.(type) {
	case *types.MsgUpdateInterchainSecurityModule:
		return msg.Proof, msg.PublicValues
	case *types.MsgSubmitMessages:
		return msg.Proof, msg.PublicValues
	}
	return nil, nil
}

// TestPreverifyVerdictIsUsed checks that the handler returns a recorded verdict for its exact inputs and
// verifies inline once any input differs.
func (suite *KeeperTestSuite) TestPreverifyVerdictIsUsed() {
	proofBz, pubValues := readStateTransitionProofData(suite.T())
	var values types.StateTransitionValues
	suite.Require().NoError(values.Unmarshal(pubValues))
	legacyVkey, err := os.ReadFile("../internal/testdata/groth16_sp1v5_test_key.bin")
	suite.Require().NoError(err)
	sentinel := errors.New("recorded verdict")

	var (
		ism       types.InterchainSecurityModule
		msgServer types.MsgServer
	)
	updateKeys := func(ctx sdk.Context, groth16Vkey, stateTransitionVkey []byte) {
		_, err := msgServer.UpdateVerifyingKeys(ctx, &types.MsgUpdateVerifyingKeys{
			Id:                  ism.Id,
			Owner:               ism.Owner,
			Groth16Vkey:         groth16Vkey,
			StateTransitionVkey: stateTransitionVkey,
			StateMembershipVkey: ism.StateMembershipVkey,
		})
		suite.Require().NoError(err)
	}

	testCases := []struct {
		name     string
		mutate   func(ctx sdk.Context, msg *types.MsgUpdateInterchainSecurityModule)
		expError error
	}{
		{
			name:     "exact inputs",
			mutate:   func(sdk.Context, *types.MsgUpdateInterchainSecurityModule) {},
			expError: sentinel,
		},
		{
			name: "proof byte",
			mutate: func(_ sdk.Context, msg *types.MsgUpdateInterchainSecurityModule) {
				msg.Proof = bytes.Clone(msg.Proof)
				msg.Proof[types.PrefixLen+types.MetadataLen-1] ^= 1 // proof nonce
			},
			expError: types.ErrInvalidProof,
		},
		{
			name: "public values",
			mutate: func(_ sdk.Context, msg *types.MsgUpdateInterchainSecurityModule) {
				msg.PublicValues = marshalStateTransitionValues(suite, pubValues, func(v *types.StateTransitionValues) { v.NewState[0] ^= 1 })
			},
			expError: types.ErrInvalidProof,
		},
		{
			name: "groth16 verifying key",
			mutate: func(ctx sdk.Context, _ *types.MsgUpdateInterchainSecurityModule) {
				updateKeys(ctx, legacyVkey, ism.StateTransitionVkey)
			},
			expError: types.ErrInvalidProofPrefix,
		},
		{
			name: "program verifying key",
			mutate: func(ctx sdk.Context, _ *types.MsgUpdateInterchainSecurityModule) {
				programVk := bytes.Clone(ism.StateTransitionVkey)
				programVk[len(programVk)-1] ^= 1
				updateKeys(ctx, ism.Groth16Vkey, programVk)
			},
			expError: types.ErrInvalidProof,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			msgServer = keeper.NewMsgServerImpl(suite.zkISMKeeper)
			ism = suite.CreateTestIsm(values.State)
			ism.Owner = testfactory.TestAccAddr
			suite.Require().NoError(suite.zkISMKeeper.SetIsm(suite.ctx, ism.Id, ism))

			ctx, _ := suite.ctx.CacheContext()
			msg := &types.MsgUpdateInterchainSecurityModule{Id: ism.Id, Proof: proofBz, PublicValues: pubValues}
			tc.mutate(ctx, msg)
			suite.zkISMKeeper.RecordVerdict(ism.Groth16Vkey, ism.StateTransitionVkey, proofBz, pubValues, sentinel)

			_, err := msgServer.UpdateInterchainSecurityModule(ctx, msg)
			suite.Require().ErrorIs(err, tc.expError)
		})
	}
}

// TestPreverifyVerdictIsUsedBySubmit checks that SubmitMessages looks up its verdict under its own inputs.
func (suite *KeeperTestSuite) TestPreverifyVerdictIsUsedBySubmit() {
	trustedState, err := hex.DecodeString(stateMembershipTrustedState)
	suite.Require().NoError(err)
	ism := suite.CreateTestIsm(trustedState)
	proofBz, pubValues := readStateMembershipProofData(suite.T())
	sentinel := errors.New("recorded verdict")
	suite.zkISMKeeper.RecordVerdict(ism.Groth16Vkey, ism.StateMembershipVkey, proofBz, pubValues, sentinel)

	msg := &types.MsgSubmitMessages{Id: ism.Id, Proof: proofBz, PublicValues: pubValues}
	_, err = keeper.NewMsgServerImpl(suite.zkISMKeeper).SubmitMessages(suite.ctx, msg)
	suite.Require().ErrorIs(err, sentinel)
}

// TestPreverifyStaleVerifyingKey checks that a verdict recorded before UpdateVerifyingKeys in the same block
// does not apply to the replaced key.
func (suite *KeeperTestSuite) TestPreverifyStaleVerifyingKey() {
	proofBz, pubValues := readStateTransitionProofData(suite.T())
	var values types.StateTransitionValues
	suite.Require().NoError(values.Unmarshal(pubValues))
	legacyVkey, err := os.ReadFile("../internal/testdata/groth16_sp1v5_test_key.bin")
	suite.Require().NoError(err)

	ism := suite.CreateTestIsm(values.State)
	ism.Owner = testfactory.TestAccAddr
	suite.Require().NoError(suite.zkISMKeeper.SetIsm(suite.ctx, ism.Id, ism))
	update := &types.MsgUpdateInterchainSecurityModule{Id: ism.Id, Proof: proofBz, PublicValues: pubValues}

	suite.zkISMKeeper.PreverifyProofs(suite.ctx, []sdk.Msg{update})
	suite.Require().True(suite.zkISMKeeper.HasVerdict(ism.Groth16Vkey, ism.StateTransitionVkey, proofBz, pubValues))

	msgServer := keeper.NewMsgServerImpl(suite.zkISMKeeper)
	_, err = msgServer.UpdateVerifyingKeys(suite.ctx, &types.MsgUpdateVerifyingKeys{
		Id:                  ism.Id,
		Owner:               ism.Owner,
		Groth16Vkey:         legacyVkey,
		StateTransitionVkey: ism.StateTransitionVkey,
		StateMembershipVkey: ism.StateMembershipVkey,
	})
	suite.Require().NoError(err)

	_, err = msgServer.UpdateInterchainSecurityModule(suite.ctx, update)
	suite.Require().ErrorIs(err, types.ErrInvalidProofPrefix)
}

// TestPreverifyConcurrentLookups checks under -race that handlers can look up verdicts while
// PreverifyProofs verifies a block of distinct proofs, as CheckTx does alongside FinalizeBlock.
func (suite *KeeperTestSuite) TestPreverifyConcurrentLookups() {
	proofBz, pubValues := readStateTransitionProofData(suite.T())
	var values types.StateTransitionValues
	suite.Require().NoError(values.Unmarshal(pubValues))
	ism := suite.CreateTestIsm(values.State)
	verifier, err := suite.zkISMKeeper.Verifier(ism.Groth16Vkey)
	suite.Require().NoError(err)

	// Distinct new states keep dedupe from collapsing the block; all but the first fail verification.
	msgs := make([]sdk.Msg, min(max(runtime.GOMAXPROCS(0), 16), appconsts.MaxZKProofMessages))
	for i := range msgs {
		values := marshalStateTransitionValues(suite, pubValues, func(v *types.StateTransitionValues) { v.NewState[0] ^= byte(i) })
		msgs[i] = &types.MsgUpdateInterchainSecurityModule{Id: ism.Id, Proof: proofBz, PublicValues: values}
	}
	suite.zkISMKeeper.PreverifyProofs(suite.ctx, msgs[:1])

	var (
		stop        atomic.Bool
		started, wg sync.WaitGroup
	)
	lookup := func() error {
		return suite.zkISMKeeper.VerifyProof(verifier, ism.Groth16Vkey, proofBz, ism.StateTransitionVkey, pubValues)
	}
	errs := make([]error, 4)
	started.Add(len(errs))
	for i := range errs {
		wg.Go(func() {
			errs[i] = lookup()
			started.Done()
			for !stop.Load() && errs[i] == nil {
				errs[i] = lookup()
			}
		})
	}
	started.Wait()
	suite.zkISMKeeper.PreverifyProofs(suite.ctx, msgs)
	stop.Store(true)
	wg.Wait()
	for _, err := range errs {
		suite.Require().NoError(err)
	}
	for _, msg := range msgs {
		proof, values := proofOf(msg)
		suite.Require().True(suite.zkISMKeeper.HasVerdict(ism.Groth16Vkey, ism.StateTransitionVkey, proof, values))
	}
}

// TestPreBlockerRecordsVerdict checks that PreBlocker finds a block's proof and records its verdict.
func (suite *KeeperTestSuite) TestPreBlockerRecordsVerdict() {
	proofBz, pubValues := readStateTransitionProofData(suite.T())
	var values types.StateTransitionValues
	suite.Require().NoError(values.Unmarshal(pubValues))
	ism := suite.CreateTestIsm(values.State)

	builder := suite.app.GetTxConfig().NewTxBuilder()
	suite.Require().NoError(builder.SetMsgs(&types.MsgUpdateInterchainSecurityModule{Id: ism.Id, Proof: proofBz, PublicValues: pubValues}))
	tx, err := suite.app.GetTxConfig().TxEncoder()(builder.GetTx())
	suite.Require().NoError(err)

	_, err = suite.app.PreBlocker(suite.ctx, &abci.RequestFinalizeBlock{Txs: [][]byte{tx}})
	suite.Require().NoError(err)
	suite.Require().True(suite.zkISMKeeper.HasVerdict(ism.Groth16Vkey, ism.StateTransitionVkey, proofBz, pubValues))
}
