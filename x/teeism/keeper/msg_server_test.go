package keeper_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/x/teeism/keeper"
	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
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
		Rtmr0:       fill(types.DigestLen),
		Rtmr1:       fill(types.DigestLen),
		Rtmr2:       fill(types.DigestLen),
		OsImageHash: fill(32),
		ComposeHash: fill(32),
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
	overrides := &types.VerifierOverrides{TrustedRootCerts: [][]byte{s.root()}}

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

func (s *KeeperTestSuite) TestGenesisKeepsOwnerAndOverrides() {
	id := s.createIsm()
	_, err := s.msgServer.UpdateOwner(s.ctx, &types.MsgUpdateOwner{Owner: s.owner, Id: id, NewOwner: s.next})
	s.Require().NoError(err)
	overrides := &types.VerifierOverrides{TrustedRootCerts: [][]byte{s.root()}}
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
	bad.Isms[0].VerifierOverrides = &types.VerifierOverrides{TrustedRootCerts: [][]byte{{0x30, 0x01}}}
	s.Require().ErrorIs(bad.Validate(), types.ErrInvalidVerifierOverrides)
}

// root returns a DER root certificate that is not Intel's.
func (s *KeeperTestSuite) root() []byte {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	s.Require().NoError(err)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Intel SGX Root CA"}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	s.Require().NoError(err)
	return der
}

// rootVectors is the tree the Rust side built: every leaf with its branch against
// the root after all seven leaves.
type rootVectors struct {
	Tree struct {
		Leaves []string `json:"leaves"`
		Root   string   `json:"root_hex"`
		Count  uint32   `json:"count"`
		Proofs []struct {
			Index uint32   `json:"index"`
			Proof []string `json:"proof"`
		} `json:"proofs"`
	} `json:"tree"`
}

func (s *KeeperTestSuite) rootVectors() rootVectors {
	raw, err := os.ReadFile("../internal/testdata/root_vectors.json")
	s.Require().NoError(err)
	var v rootVectors
	s.Require().NoError(json.Unmarshal(raw, &v))
	return v
}

func unhex0x(s string) []byte {
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		panic(err)
	}
	return b
}

func (s *KeeperTestSuite) metadata(v rootVectors, index int, root []byte) []byte {
	m := &types.DeliveryMetadata{Index: v.Tree.Proofs[index].Index}
	copy(m.Root[:], root)
	for i, p := range v.Tree.Proofs[index].Proof {
		copy(m.Proof[i][:], unhex0x(p))
	}
	return types.EncodeDeliveryMetadata(m)
}

// attest records roots for an ism the way SubmitAttestation does, through the
// genesis path, since no real quote commits to a synthetic root.
func (s *KeeperTestSuite) attest(id util.HexAddress, roots ...[]byte) {
	gs, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	at := -1
	for i := range gs.Roots {
		if gs.Roots[i].Id.Compare(id) == 0 {
			at = i
		}
	}
	if at < 0 {
		gs.Roots = append(gs.Roots, types.GenesisRoots{Id: id})
		at = len(gs.Roots) - 1
	}
	for _, r := range roots {
		gs.Roots[at].Roots = append(gs.Roots[at].Roots, types.EncodeHex(r))
	}
	for i := range gs.Isms {
		if gs.Isms[i].Id.Compare(id) == 0 {
			gs.Isms[i].TreeRoot = roots[len(roots)-1]
			gs.Isms[i].TreeCount = 7
		}
	}
	s.Require().NoError(gs.Validate())
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, gs))
}

func (s *KeeperTestSuite) TestVerifyAgainstAttestedRoots() {
	v := s.rootVectors()
	root := unhex0x(v.Tree.Root)
	id := s.createIsm()

	// The message id is what the branch folds; util.HyperlaneMessage.Id() is
	// keccak of the encoding, so Verify is driven through the metadata type at
	// the id level and through the keeper for the root bookkeeping.
	leaf := func(i int) [32]byte {
		var l [32]byte
		copy(l[:], unhex0x(v.Tree.Leaves[i]))
		return l
	}
	for i := range v.Tree.Leaves {
		m, err := types.DecodeDeliveryMetadata(s.metadata(v, i, root))
		s.Require().NoError(err)
		s.Require().True(m.Proves(leaf(i)))
	}

	// Nothing attested yet: a perfect proof against an unknown root is refused.
	msg := util.HyperlaneMessage{Version: 3, Origin: 7, Destination: 1, Body: []byte("x")}
	ok, err := s.keeper.Verify(s.ctx, id, s.metadata(v, 0, root), msg)
	s.Require().NoError(err)
	s.Require().False(ok)

	s.attest(id, root)
	s.Require().Equal(root, s.getIsm(id).TreeRoot)
	s.Require().Equal(uint32(7), s.getIsm(id).TreeCount)

	// The root is known, but this message is not one of the leaves.
	ok, err = s.keeper.Verify(s.ctx, id, s.metadata(v, 0, root), msg)
	s.Require().NoError(err)
	s.Require().False(ok)

	// Malformed metadata is a refusal, not an error.
	ok, err = s.keeper.Verify(s.ctx, id, []byte{1, 2, 3}, msg)
	s.Require().NoError(err)
	s.Require().False(ok)
	ok, err = s.keeper.Verify(s.ctx, id, nil, msg)
	s.Require().NoError(err)
	s.Require().False(ok)

	// An unknown ism is an error.
	_, err = s.keeper.Verify(s.ctx, util.CreateMockHexAddress("missing", 9), s.metadata(v, 0, root), msg)
	s.Require().ErrorIs(err, types.ErrIsmNotFound)

	// Every attested root stays accepted after a newer one lands.
	newer := bytes.Repeat([]byte{0xee}, 32)
	s.attest(id, newer)
	s.Require().Equal(newer, s.getIsm(id).TreeRoot)
	res, err := keeper.NewQueryServerImpl(s.keeper).Roots(s.ctx, &types.QueryRootsRequest{Id: id.String()})
	s.Require().NoError(err)
	s.Require().ElementsMatch([]string{types.EncodeHex(root), types.EncodeHex(newer)}, res.Roots)
}

// The whole Verify path with real messages: two messages inserted into a Hyperlane
// tree, each delivered with its sibling and the zero hashes above.
func (s *KeeperTestSuite) TestVerifyDeliversMessages() {
	id := s.createIsm()
	msgs := []util.HyperlaneMessage{
		{Version: 3, Nonce: 1, Origin: 7, Destination: 1, Body: []byte("first")},
		{Version: 3, Nonce: 2, Origin: 7, Destination: 1, Body: []byte("second")},
	}
	tree := util.NewTree([util.TreeDepth][32]byte{}, 0)
	var leaves [2][32]byte
	for i, m := range msgs {
		copy(leaves[i][:], m.Id().Bytes())
		s.Require().NoError(tree.Insert(leaves[i]))
	}
	root := tree.GetRoot()
	proofFor := func(i int) []byte {
		m := &types.DeliveryMetadata{Root: root, Index: uint32(i), Proof: util.ZeroHashes}
		m.Proof[0] = leaves[1-i]
		return types.EncodeDeliveryMetadata(m)
	}

	ok, err := s.keeper.Verify(s.ctx, id, proofFor(0), msgs[0])
	s.Require().NoError(err)
	s.Require().False(ok, "the root is not attested yet")

	s.attest(id, root[:])
	for i, m := range msgs {
		ok, err := s.keeper.Verify(s.ctx, id, proofFor(i), m)
		s.Require().NoError(err)
		s.Require().True(ok, "message %d", i)

		// Verify writes nothing, so delivering twice is the mailbox's problem to refuse.
		ok, err = s.keeper.Verify(s.ctx, id, proofFor(i), m)
		s.Require().NoError(err)
		s.Require().True(ok)
	}
	ok, err = s.keeper.Verify(s.ctx, id, proofFor(1), msgs[0])
	s.Require().NoError(err)
	s.Require().False(ok, "the other message's branch")
	forged := msgs[0]
	forged.Body = []byte("forged")
	ok, err = s.keeper.Verify(s.ctx, id, proofFor(0), forged)
	s.Require().NoError(err)
	s.Require().False(ok, "a message that was never dispatched")
}

func (s *KeeperTestSuite) TestGenesisCarriesRoots() {
	v := s.rootVectors()
	root := unhex0x(v.Tree.Root)
	id := s.createIsm()
	s.attest(id, root)

	gs, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(gs.Validate())
	s.Require().Len(gs.Roots, 1)
	s.Require().Equal([]string{types.EncodeHex(root)}, gs.Roots[0].Roots)

	bad := *gs
	bad.Roots = append([]types.GenesisRoots{}, gs.Roots...)
	bad.Roots[0].Roots = []string{"0x0102"}
	s.Require().Error(bad.Validate(), "a root must be 32 bytes")
	bad.Roots[0] = types.GenesisRoots{Id: util.CreateMockHexAddress("missing", 9), Roots: []string{types.EncodeHex(root)}}
	s.Require().ErrorIs(bad.Validate(), types.ErrIsmNotFound)

	s.SetupTest()
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, gs))
	s.Require().Equal(root, s.getIsm(id).TreeRoot)
	msg := util.HyperlaneMessage{Version: 3, Origin: 7, Destination: 1, Body: []byte("x")}
	ok, err := s.keeper.Verify(s.ctx, id, s.metadata(v, 0, root), msg)
	s.Require().NoError(err)
	s.Require().False(ok, "known root, but not this message")
}
