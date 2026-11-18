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
	"github.com/cosmos/cosmos-sdk/codec"
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
	cdc       codec.Codec

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
	s.cdc = testApp.AppCodec()

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
		Identities:        []*types.EnclaveIdentity{id},
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
	_, err = s.msgServer.SetEnclaveIdentities(s.ctx, &types.MsgSetEnclaveIdentities{Owner: s.owner, Id: id, Identities: []*types.EnclaveIdentity{identity(2)}})
	s.Require().ErrorIs(err, sdkerrors.ErrUnauthorized)
	_, err = s.msgServer.SetEnclaveIdentities(s.ctx, &types.MsgSetEnclaveIdentities{Owner: s.next, Id: id, Identities: []*types.EnclaveIdentity{identity(2)}})
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

// attest records roots on an ism in order, the way SubmitAttestation does,
// since no real quote commits to a synthetic root.
func (s *KeeperTestSuite) attest(id util.HexAddress, roots ...[]byte) {
	for _, r := range roots {
		s.Require().NoError(s.keeper.RecordRootForTest(s.ctx, id, r))
	}
}

// syntheticRoots returns n distinct 32-byte roots.
func syntheticRoots(n int) [][]byte {
	roots := make([][]byte, n)
	for i := range roots {
		r := make([]byte, 32)
		r[0], r[1] = byte(i>>8), byte(i)
		r[31] = 1
		roots[i] = r
	}
	return roots
}

// window is the roots an ism holds, oldest first, as genesis exports them.
func (s *KeeperTestSuite) window(id util.HexAddress) []string {
	gs, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	for _, entry := range gs.Roots {
		if entry.Id.Compare(id) == 0 {
			return entry.Roots
		}
	}
	return nil
}

func hexAll(roots [][]byte) []string {
	out := make([]string, len(roots))
	for i, r := range roots {
		out[i] = types.EncodeHex(r)
	}
	return out
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

// A rotation through the msg server: {1} -> {1, 2} -> {2}. The state keeps its
// root and height, carries the set digest, and the set is queryable in canonical
// order.
func (s *KeeperTestSuite) TestSetEnclaveIdentities() {
	id := s.createIsm()
	before, err := types.DecodeIsmState(s.getIsm(id).State)
	s.Require().NoError(err)

	_, err = s.msgServer.SetEnclaveIdentities(s.ctx, &types.MsgSetEnclaveIdentities{Owner: s.stranger, Id: id, Identities: []*types.EnclaveIdentity{identity(2)}})
	s.Require().ErrorIs(err, sdkerrors.ErrUnauthorized)

	_, err = s.msgServer.SetEnclaveIdentities(s.ctx, &types.MsgSetEnclaveIdentities{Owner: s.owner, Id: id, Identities: []*types.EnclaveIdentity{identity(2), identity(1)}})
	s.Require().NoError(err)
	s.Require().True(s.hasEvent(&types.EventSetEnclaveIdentities{}))

	ism := s.getIsm(id)
	s.Require().Len(ism.Identities, 2)
	want, err := types.IdentitySetDigest([]*types.EnclaveIdentity{identity(1), identity(2)})
	s.Require().NoError(err)
	got, err := ism.IdentityDigest()
	s.Require().NoError(err)
	s.Require().Equal(want, got)
	after, err := types.DecodeIsmState(ism.State)
	s.Require().NoError(err)
	s.Require().Equal(want, after.IdentityDigest)
	s.Require().Equal(before.StateRoot, after.StateRoot)
	s.Require().Equal(before.Height, after.Height)

	_, err = s.msgServer.SetEnclaveIdentities(s.ctx, &types.MsgSetEnclaveIdentities{Owner: s.owner, Id: id, Identities: []*types.EnclaveIdentity{identity(2)}})
	s.Require().NoError(err)
	ism = s.getIsm(id)
	s.Require().Equal([]*types.EnclaveIdentity{identity(2)}, ism.Identities)
	final, err := types.DecodeIsmState(ism.State)
	s.Require().NoError(err)
	s.Require().Equal(identity(2).Digest(), final.IdentityDigest)

	_, err = s.msgServer.SetEnclaveIdentities(s.ctx, &types.MsgSetEnclaveIdentities{Owner: s.owner, Id: id, Identities: []*types.EnclaveIdentity{identity(3), identity(3)}})
	s.Require().ErrorIs(err, types.ErrInvalidEnclaveIdentity)
}

// An ism can be created pinning several enclaves at once; the state must carry
// their set digest, not any one member's.
func (s *KeeperTestSuite) TestCreateWithASet() {
	set := []*types.EnclaveIdentity{identity(4), identity(5)}
	digest, err := types.IdentitySetDigest(set)
	s.Require().NoError(err)

	for _, tc := range []struct {
		name   string
		digest [32]byte
		ok     bool
	}{
		{"the set digest", digest, true},
		{"one member's digest", identity(4).Digest(), false},
	} {
		state := &types.IsmState{OriginDomain: 7, Height: 100, Timestamp: 1000, IdentityDigest: tc.digest}
		state.StateRoot[0] = 1
		msg := &types.MsgCreateInterchainSecurityModule{
			Creator:           s.owner,
			State:             types.EncodeIsmState(state),
			MerkleTreeAddress: bytes.Repeat([]byte{9}, 32),
			Identities:        set,
		}
		err := msg.ValidateBasic()
		if tc.ok {
			s.Require().NoError(err, tc.name)
			res, err := s.msgServer.CreateInterchainSecurityModule(s.ctx, msg)
			s.Require().NoError(err)
			s.Require().Len(s.getIsm(res.Id).Identities, 2)
		} else {
			s.Require().ErrorIs(err, types.ErrInvalidEnclaveIdentity, tc.name)
		}
	}
}

// More roots than one page of a paginated query: an export must carry every one,
// or a fresh import loses roots that messages were proven against.
func (s *KeeperTestSuite) TestGenesisExportsTheWindow() {
	id := s.createIsm()
	_, err := s.msgServer.SetRootRetention(s.ctx, &types.MsgSetRootRetention{Owner: s.owner, Id: id, Retention: types.MaxRootRetention})
	s.Require().NoError(err)
	const n = types.MaxRootRetention + 50
	roots := syntheticRoots(n)
	s.attest(id, roots...)

	gs, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(gs.Validate())
	s.Require().Len(gs.Roots, 1)
	s.Require().Equal(hexAll(roots[n-types.MaxRootRetention:]), gs.Roots[0].Roots, "the newest roots, oldest first")

	// Through JSON, as an export reaches a new chain.
	bz, err := s.cdc.MarshalJSON(gs)
	s.Require().NoError(err)
	var decoded types.GenesisState
	s.Require().NoError(s.cdc.UnmarshalJSON(bz, &decoded))
	s.Require().NoError(decoded.Validate())

	s.SetupTest()
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, &decoded))
	again, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(gs.Roots, again.Roots)
	s.Require().Equal(uint64(n), s.getIsm(id).RootSeq)

	// The imported window keeps pruning in order: the next root evicts the oldest.
	next := syntheticRoots(n + 1)[n]
	s.attest(id, next)
	s.Require().Equal(append(hexAll(roots[n-types.MaxRootRetention+1:]), types.EncodeHex(next)), s.window(id))
}

func (s *KeeperTestSuite) TestRootRetentionWindow() {
	v := s.rootVectors()
	root := unhex0x(v.Tree.Root)
	id := s.createIsm()
	ism := s.getIsm(id)
	s.Require().Equal(uint64(types.DefaultRootRetention), ism.Retention())
	msg := util.HyperlaneMessage{Version: 3, Origin: 7, Destination: 1, Body: []byte("x")}
	metadata := s.metadata(v, 0, root)
	known := func() bool {
		gs, err := s.keeper.ExportGenesis(s.ctx)
		s.Require().NoError(err)
		for _, entry := range gs.Roots {
			for _, r := range entry.Roots {
				if r == types.EncodeHex(root) {
					return true
				}
			}
		}
		return false
	}

	// A root stays accepted while it is among the newest DefaultRootRetention.
	s.attest(id, root)
	later := syntheticRoots(types.DefaultRootRetention)
	s.attest(id, later[:types.DefaultRootRetention-1]...)
	s.Require().True(known(), "the root is the oldest of a full window")
	s.Require().Len(s.window(id), types.DefaultRootRetention)
	_, err := s.keeper.Verify(s.ctx, id, metadata, msg)
	s.Require().NoError(err)

	// Re-attesting a root the window holds, as a heartbeat does, prunes nothing.
	s.attest(id, later[types.DefaultRootRetention-2])
	s.Require().True(known())

	// One more root pushes it out, and a proof against it is refused.
	s.attest(id, later[types.DefaultRootRetention-1])
	s.Require().False(known(), "the root was pruned")
	s.Require().Len(s.window(id), types.DefaultRootRetention)
	ok, err := s.keeper.Verify(s.ctx, id, metadata, msg)
	s.Require().NoError(err)
	s.Require().False(ok)
}

func (s *KeeperTestSuite) TestSetRootRetention() {
	id := s.createIsm()
	roots := syntheticRoots(40)
	s.attest(id, roots...)
	s.Require().Equal(hexAll(roots[40-types.DefaultRootRetention:]), s.window(id))

	set := func(owner string, retention uint32) error {
		_, err := s.msgServer.SetRootRetention(s.ctx, &types.MsgSetRootRetention{Owner: owner, Id: id, Retention: retention})
		return err
	}
	s.Require().Error(set(s.stranger, 8), "only the owner")
	s.Require().Error(set(s.owner, 0))
	s.Require().Error(set(s.owner, types.MaxRootRetention+1))
	ism := s.getIsm(id)
	s.Require().Equal(uint64(types.DefaultRootRetention), ism.Retention())

	// Lowering prunes the oldest at once and emits the change.
	s.Require().NoError(set(s.owner, 4))
	s.Require().Equal(hexAll(roots[36:]), s.window(id))
	s.Require().True(s.hasEvent(&types.EventSetRootRetention{Id: id, Owner: s.owner, PreviousRetention: types.DefaultRootRetention, Retention: 4}))

	// Raising keeps what is held and lets the window grow again.
	s.Require().NoError(set(s.owner, types.MaxRootRetention))
	s.Require().Equal(hexAll(roots[36:]), s.window(id))
	more := syntheticRoots(60)[40:]
	s.attest(id, more...)
	s.Require().Len(s.window(id), 24)

	// A retention of one keeps only the newest root.
	s.Require().NoError(set(s.owner, 1))
	s.Require().Equal([]string{types.EncodeHex(more[len(more)-1])}, s.window(id))

	_, err := s.msgServer.SetRootRetention(s.ctx, &types.MsgSetRootRetention{Owner: s.owner, Id: util.CreateMockHexAddress("missing", 9), Retention: 8})
	s.Require().ErrorIs(err, types.ErrIsmNotFound)
}

// After the owner lowers the retention and raises it again, the window holds
// fewer roots than the retention allows until new roots fill it; it still
// exports, imports and keeps pruning in order.
func (s *KeeperTestSuite) TestGenesisExportsAWindowBelowItsRetention() {
	id := s.createIsm()
	roots := syntheticRoots(40)
	s.attest(id, roots...)
	set := func(retention uint32) {
		_, err := s.msgServer.SetRootRetention(s.ctx, &types.MsgSetRootRetention{Owner: s.owner, Id: id, Retention: retention})
		s.Require().NoError(err)
	}
	set(4)
	set(types.MaxRootRetention)
	s.Require().Equal(hexAll(roots[36:]), s.window(id))

	gs, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(gs.Validate())
	bz, err := s.cdc.MarshalJSON(gs)
	s.Require().NoError(err)
	var decoded types.GenesisState
	s.Require().NoError(s.cdc.UnmarshalJSON(bz, &decoded))
	s.Require().NoError(decoded.Validate())

	s.SetupTest()
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, &decoded))
	s.Require().Equal(hexAll(roots[36:]), s.window(id))
	next := syntheticRoots(41)[40]
	s.attest(id, next)
	s.Require().Equal(append(hexAll(roots[36:]), types.EncodeHex(next)), s.window(id), "the window grows again")
}

func (s *KeeperTestSuite) TestGenesisRejectsInconsistentWindow() {
	id := s.createIsm()
	roots := syntheticRoots(3)
	s.attest(id, roots...)
	gs, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(gs.Validate())

	mutate := func(f func(gs *types.GenesisState)) error {
		bz, err := s.cdc.MarshalJSON(gs)
		s.Require().NoError(err)
		var c types.GenesisState
		s.Require().NoError(s.cdc.UnmarshalJSON(bz, &c))
		f(&c)
		return c.Validate()
	}
	s.Require().Error(mutate(func(gs *types.GenesisState) { gs.Roots[0].Roots = nil }), "an empty window")
	s.Require().Error(mutate(func(gs *types.GenesisState) { gs.Isms[0].RootSeq = 2 }), "more roots than were attested")
	s.Require().Error(mutate(func(gs *types.GenesisState) { gs.Roots[0].Roots[0] = gs.Roots[0].Roots[1] }), "a root listed twice")
	s.Require().Error(mutate(func(gs *types.GenesisState) {
		r := gs.Roots[0].Roots
		r[1], r[2] = r[2], r[1]
	}), "the newest root out of place")
	s.Require().Error(mutate(func(gs *types.GenesisState) { gs.Roots = nil }), "an attested ism with no roots")
	s.Require().Error(mutate(func(gs *types.GenesisState) { gs.Isms[0].RootRetention = types.MaxRootRetention + 1 }))
	s.Require().Error(mutate(func(gs *types.GenesisState) { gs.Isms[0].RootRetention = 2 }), "more roots than the retention")
}

// Every handler checks its message itself, whether or not ValidateBasic ran first,
// and refuses before reading state, allocating an id or metering verification.
func (s *KeeperTestSuite) TestHandlersValidateTheirMessages() {
	id := s.createIsm()
	before, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)

	state := &types.IsmState{OriginDomain: 7, Height: 100, Timestamp: 1000, IdentityDigest: identity(1).Digest()}
	_, err = s.msgServer.CreateInterchainSecurityModule(s.ctx, &types.MsgCreateInterchainSecurityModule{
		Creator:           s.owner,
		State:             types.EncodeIsmState(state)[:types.IsmStateBytes-1],
		MerkleTreeAddress: bytes.Repeat([]byte{9}, 32),
		Identities:        []*types.EnclaveIdentity{identity(1)},
	})
	s.Require().ErrorIs(err, types.ErrInvalidTrustedState)

	_, err = s.msgServer.CreateInterchainSecurityModule(s.ctx, &types.MsgCreateInterchainSecurityModule{
		Creator:           "not-an-address",
		State:             types.EncodeIsmState(state),
		MerkleTreeAddress: bytes.Repeat([]byte{9}, 32),
		Identities:        []*types.EnclaveIdentity{identity(1)},
	})
	s.Require().ErrorIs(err, sdkerrors.ErrInvalidAddress)

	// Collateral is optional on the wire; leaving it out is an error, not a panic.
	_, err = s.msgServer.SubmitAttestation(s.ctx, &types.MsgSubmitAttestation{
		Id:       id,
		Signer:   s.owner,
		Quote:    []byte{1},
		EventLog: []byte{1},
		Payload:  make([]byte, types.AttestedUpdateBytes),
	})
	s.Require().ErrorIs(err, types.ErrInvalidCollateral)

	_, err = s.msgServer.SubmitAttestation(s.ctx, &types.MsgSubmitAttestation{
		Id:       id,
		Signer:   "not-an-address",
		Quote:    []byte{1},
		EventLog: []byte{1},
		Payload:  make([]byte, types.AttestedUpdateBytes),
	})
	s.Require().ErrorIs(err, sdkerrors.ErrInvalidAddress)

	gasBefore := s.ctx.GasMeter().GasConsumed()
	_, err = s.msgServer.SubmitAttestation(s.ctx, &types.MsgSubmitAttestation{
		Id:       id,
		Signer:   s.owner,
		Quote:    make([]byte, types.MaxQuoteBytes+1),
		EventLog: []byte{1},
		Payload:  make([]byte, types.AttestedUpdateBytes),
	})
	s.Require().ErrorIs(err, types.ErrMessageTooLarge)
	s.Require().Less(s.ctx.GasMeter().GasConsumed()-gasBefore, uint64(types.DefaultAttestationVerifyCost),
		"refused before metering verification")

	_, err = s.msgServer.UpdateOwner(s.ctx, &types.MsgUpdateOwner{Owner: s.owner, Id: id, NewOwner: "not-an-address"})
	s.Require().ErrorIs(err, sdkerrors.ErrInvalidAddress)

	_, err = s.msgServer.SetEnclaveIdentities(s.ctx, &types.MsgSetEnclaveIdentities{Owner: "not-an-address", Id: id, Identities: []*types.EnclaveIdentity{identity(2)}})
	s.Require().ErrorIs(err, sdkerrors.ErrInvalidAddress)

	_, err = s.msgServer.SetVerifierOverrides(s.ctx, &types.MsgSetVerifierOverrides{Owner: "not-an-address", Id: id})
	s.Require().ErrorIs(err, sdkerrors.ErrInvalidAddress)

	after, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(before, after, "nothing was written")
}
