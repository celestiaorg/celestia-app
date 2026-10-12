package keeper_test

import (
	"bytes"
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/x/teeism/keeper"
	"github.com/celestiaorg/celestia-app/v10/x/teeism/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/proto/tendermint/version"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/suite"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx       sdk.Context
	keeper    *keeper.Keeper
	msgServer types.MsgServer

	owner, next, stranger string
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (s *KeeperTestSuite) SetupTest() {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	s.ctx = testApp.NewUncachedContext(false, cmtproto.Header{Version: version.Consensus{App: appconsts.Version}})
	s.keeper = testApp.TeeIsmKeeper
	s.msgServer = keeper.NewMsgServerImpl(s.keeper)

	addr := func(b byte) string { return sdk.AccAddress(bytes.Repeat([]byte{b}, 20)).String() }
	s.owner, s.next, s.stranger = addr(1), addr(2), addr(3)
}

func identity(b byte) *types.EnclaveIdentity {
	fill := func(n int) []byte { return bytes.Repeat([]byte{b}, n) }
	return &types.EnclaveIdentity{
		MrTd:        fill(types.MrTdLen),
		OsImageHash: fill(32),
		ComposeHash: fill(32),
		MrKms:       fill(32),
		KeyProvider: fill(16),
	}
}

func (s *KeeperTestSuite) createIsm() util.HexAddress {
	id := identity(1)
	state := &types.IsmState{OriginDomain: 7, Height: 100, Timestamp: 1000, IdentityDigest: id.Digest()}
	state.StateRoot[0] = 1

	res, err := s.msgServer.CreateInterchainSecurityModule(s.ctx, &types.MsgCreateInterchainSecurityModule{
		Creator:           s.owner,
		State:             types.EncodeIsmState(state),
		MerkleTreeAddress: bytes.Repeat([]byte{9}, 32),
		Identity:          id,
	})
	s.Require().NoError(err)
	return res.Id
}

func (s *KeeperTestSuite) getIsm(id util.HexAddress) types.InterchainSecurityModule {
	res, err := keeper.NewQueryServerImpl(s.keeper).Ism(s.ctx, &types.QueryIsmRequest{Id: id.String()})
	s.Require().NoError(err)
	return res.Ism
}

func (s *KeeperTestSuite) hasEvent(event proto.Message) bool {
	for _, e := range s.ctx.EventManager().Events() {
		if e.Type == proto.MessageName(event) {
			return true
		}
	}
	return false
}

func (s *KeeperTestSuite) TestUpdateOwner() {
	id := s.createIsm()

	_, err := s.msgServer.UpdateOwner(s.ctx, &types.MsgUpdateOwner{Owner: s.stranger, Id: id, NewOwner: s.stranger})
	s.Require().ErrorIs(err, sdkerrors.ErrUnauthorized)

	_, err = s.msgServer.UpdateOwner(s.ctx, &types.MsgUpdateOwner{Owner: s.owner, Id: id, NewOwner: s.next})
	s.Require().NoError(err)
	s.Require().Equal(s.next, s.getIsm(id).Owner)
	s.Require().True(s.hasEvent(&types.EventUpdateOwner{}))

	// The previous owner can no longer re-pin; the new one can.
	_, err = s.msgServer.UpdateEnclaveIdentity(s.ctx, &types.MsgUpdateEnclaveIdentity{Owner: s.owner, Id: id, Identity: identity(2)})
	s.Require().ErrorIs(err, sdkerrors.ErrUnauthorized)
	_, err = s.msgServer.UpdateEnclaveIdentity(s.ctx, &types.MsgUpdateEnclaveIdentity{Owner: s.next, Id: id, Identity: identity(2)})
	s.Require().NoError(err)

	_, err = s.msgServer.UpdateOwner(s.ctx, &types.MsgUpdateOwner{Owner: s.next, Id: util.CreateMockHexAddress("missing", 9), NewOwner: s.owner})
	s.Require().ErrorIs(err, types.ErrIsmNotFound)
}

func (s *KeeperTestSuite) TestSetVerifierOverrides() {
	id := s.createIsm()
	overrides := &types.VerifierOverrides{CollateralGraceSecs: 72 * 60 * 60}

	_, err := s.msgServer.SetVerifierOverrides(s.ctx, &types.MsgSetVerifierOverrides{Owner: s.stranger, Id: id, Overrides: overrides})
	s.Require().ErrorIs(err, sdkerrors.ErrUnauthorized)
	s.Require().Nil(s.getIsm(id).VerifierOverrides)

	_, err = s.msgServer.SetVerifierOverrides(s.ctx, &types.MsgSetVerifierOverrides{Owner: s.owner, Id: id, Overrides: overrides})
	s.Require().NoError(err)
	s.Require().Equal(overrides, s.getIsm(id).VerifierOverrides, "overrides are stored and queryable")
	s.Require().True(s.hasEvent(&types.EventSetVerifierOverrides{}))

	_, err = s.msgServer.SetVerifierOverrides(s.ctx, &types.MsgSetVerifierOverrides{Owner: s.owner, Id: id})
	s.Require().NoError(err)
	s.Require().Nil(s.getIsm(id).VerifierOverrides, "unset overrides clear them")
}

func (s *KeeperTestSuite) TestOnlyTheOwnerSubmitsWhileSkippingDcap() {
	id := s.createIsm()
	_, err := s.msgServer.SetVerifierOverrides(s.ctx, &types.MsgSetVerifierOverrides{
		Owner: s.owner, Id: id, Overrides: &types.VerifierOverrides{SkipDcap: true},
	})
	s.Require().NoError(err)

	submit := func(signer string) error {
		_, err := s.msgServer.SubmitAttestation(s.ctx, &types.MsgSubmitAttestation{
			Id: id, Signer: signer, Quote: []byte{1}, EventLog: []byte{2}, Payload: []byte{3},
			Collateral: &types.Collateral{PckCrl: []byte{4}, RootCaCrl: []byte{5}},
		})
		return err
	}

	// Anyone but the owner is refused before the quote is even looked at.
	s.Require().ErrorIs(submit(s.stranger), sdkerrors.ErrUnauthorized)
	// The owner gets past authorization, and this junk quote then fails to parse.
	s.Require().ErrorIs(submit(s.owner), types.ErrInvalidCollateral)
}

func (s *KeeperTestSuite) TestGenesisKeepsOwnerAndOverrides() {
	id := s.createIsm()
	_, err := s.msgServer.UpdateOwner(s.ctx, &types.MsgUpdateOwner{Owner: s.owner, Id: id, NewOwner: s.next})
	s.Require().NoError(err)
	overrides := &types.VerifierOverrides{CollateralGraceSecs: 3600, SkipDcap: true}
	_, err = s.msgServer.SetVerifierOverrides(s.ctx, &types.MsgSetVerifierOverrides{Owner: s.next, Id: id, Overrides: overrides})
	s.Require().NoError(err)

	gs, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(gs.Validate())

	s.SetupTest()
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, gs))
	ism := s.getIsm(id)
	s.Require().Equal(s.next, ism.Owner)
	s.Require().Equal(overrides, ism.VerifierOverrides)
}

func (s *KeeperTestSuite) TestGenesisRejectsInvalidOwnerAndOverrides() {
	id := s.createIsm()
	gs, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)

	bad := *gs
	bad.Isms = append([]types.InterchainSecurityModule{}, gs.Isms...)
	bad.Isms[0].Owner = "not-an-address"
	s.Require().ErrorIs(bad.Validate(), sdkerrors.ErrInvalidAddress, "ism %s", id)

	bad.Isms[0].Owner = s.owner
	bad.Isms[0].VerifierOverrides = &types.VerifierOverrides{CollateralGraceSecs: types.MaxCollateralGraceSecs + 1}
	s.Require().ErrorIs(bad.Validate(), types.ErrInvalidVerifierOverrides)
}
