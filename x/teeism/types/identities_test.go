package types_test

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
	"github.com/stretchr/testify/require"
)

func identityFor(b byte) *types.EnclaveIdentity {
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

func ismPinnedTo(ids ...*types.EnclaveIdentity) types.InterchainSecurityModule {
	digest, err := types.IdentitySetDigest(ids)
	if err != nil {
		panic(err)
	}
	state := &types.IsmState{
		OriginDomain:   7,
		Height:         100,
		Timestamp:      1000,
		IdentityDigest: digest,
	}
	state.StateRoot[0] = 1
	state.LcStoreCommit[0] = 2
	sorted, err := types.SortIdentities(ids)
	if err != nil {
		panic(err)
	}
	return types.InterchainSecurityModule{Owner: "celestia1owner", State: types.EncodeIsmState(state), Identities: sorted}
}

func unhex32(t *testing.T, s string) [32]byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	require.Len(t, b, 32)
	var out [32]byte
	copy(out[:], b)
	return out
}

// The EVM ISM (TeeDcapIsm) hashes the same bytes from the same sorted digests and
// pins these same vectors, so the two chains agree on what a set is called.
func TestSetDigestVectors(t *testing.T) {
	d := func(b byte) [32]byte {
		var out [32]byte
		copy(out[:], bytes.Repeat([]byte{b}, 32))
		return out
	}
	require.Equal(t, unhex32(t, "ec1daf6eea2286feaecc70758f647a8e318b73f711043bffd2d6bcb0a5b41807"),
		types.DigestOfSortedDigests([][32]byte{d(0x11), d(0x22)}))
	require.Equal(t, unhex32(t, "2fa9b2f90d7deb2663ea2c8b1bd32985ddac3c536c93656d3cf92ba14676d77e"),
		types.DigestOfSortedDigests([][32]byte{d(0x11), d(0x22), d(0x33)}))
	require.Equal(t, d(0x11), types.DigestOfSortedDigests([][32]byte{d(0x11)}), "a set of one is its member")
}

func TestASetOfOneHasItsMembersDigest(t *testing.T) {
	id := identityFor(1)
	got, err := types.IdentitySetDigest([]*types.EnclaveIdentity{id})
	require.NoError(t, err)
	require.Equal(t, id.Digest(), got)
}

func TestTheSetDigestIgnoresOrder(t *testing.T) {
	a, b, c := identityFor(1), identityFor(2), identityFor(3)
	one, err := types.IdentitySetDigest([]*types.EnclaveIdentity{a, b, c})
	require.NoError(t, err)
	two, err := types.IdentitySetDigest([]*types.EnclaveIdentity{c, a, b})
	require.NoError(t, err)
	require.Equal(t, one, two)

	pair, err := types.IdentitySetDigest([]*types.EnclaveIdentity{a, b})
	require.NoError(t, err)
	require.NotEqual(t, one, pair, "a different set has a different digest")
	require.NotEqual(t, a.Digest(), pair)
}

func TestBadSetsAreRefused(t *testing.T) {
	_, err := types.IdentitySetDigest(nil)
	require.ErrorIs(t, err, types.ErrInvalidEnclaveIdentity, "empty")

	_, err = types.IdentitySetDigest([]*types.EnclaveIdentity{identityFor(1), identityFor(1)})
	require.ErrorIs(t, err, types.ErrInvalidEnclaveIdentity, "duplicate")

	bad := identityFor(2)
	bad.MrTd = bad.MrTd[:10]
	_, err = types.IdentitySetDigest([]*types.EnclaveIdentity{identityFor(1), bad})
	require.ErrorIs(t, err, types.ErrInvalidEnclaveIdentity, "invalid member")

	_, err = types.IdentitySetDigest([]*types.EnclaveIdentity{identityFor(1), nil})
	require.ErrorIs(t, err, types.ErrInvalidEnclaveIdentity, "nil member")

	many := make([]*types.EnclaveIdentity, types.MaxEnclaveIdentities+1)
	for i := range many {
		many[i] = identityFor(byte(i + 1))
	}
	_, err = types.IdentitySetDigest(many)
	require.ErrorIs(t, err, types.ErrInvalidEnclaveIdentity, "too many")
	_, err = types.IdentitySetDigest(many[:types.MaxEnclaveIdentities])
	require.NoError(t, err, "the maximum is allowed")
}

func TestOnlyTheOwnerSetsIdentities(t *testing.T) {
	old, next := identityFor(1), identityFor(2)
	ism := ismPinnedTo(old)
	before := append([]byte(nil), ism.State...)

	_, err := ism.SetIdentities("celestia1someoneelse", []*types.EnclaveIdentity{next})
	require.Error(t, err)
	_, err = ism.SetIdentities("", []*types.EnclaveIdentity{next})
	require.Error(t, err)
	require.Equal(t, before, ism.State, "a refused change changes nothing")
	require.Equal(t, []*types.EnclaveIdentity{old}, ism.Identities)

	unowned := ismPinnedTo(old)
	unowned.Owner = ""
	_, err = unowned.SetIdentities("", []*types.EnclaveIdentity{next})
	require.Error(t, err, "an ism without an owner cannot be repinned by anyone")
}

// A rotation: {old} -> {old, new} -> {new}. Each step swaps only the identity
// digest the state carries.
func TestSettingIdentitiesSwapsOnlyTheIdentityDigest(t *testing.T) {
	old, next := identityFor(1), identityFor(2)
	ism := ismPinnedTo(old)
	before, err := types.DecodeIsmState(ism.State)
	require.NoError(t, err)

	previous, err := ism.SetIdentities("celestia1owner", []*types.EnclaveIdentity{next, old})
	require.NoError(t, err)
	require.Equal(t, old.Digest(), previous)
	both, err := types.IdentitySetDigest([]*types.EnclaveIdentity{old, next})
	require.NoError(t, err)
	require.Len(t, ism.Identities, 2)
	first, second := ism.Identities[0].Digest(), ism.Identities[1].Digest()
	require.Negative(t, bytes.Compare(first[:], second[:]), "stored in ascending digest order")

	after, err := types.DecodeIsmState(ism.State)
	require.NoError(t, err)
	require.Equal(t, both, after.IdentityDigest)
	// Everything the light client and the transition rules depend on is untouched.
	require.Equal(t, before.StateRoot, after.StateRoot)
	require.Equal(t, before.OriginDomain, after.OriginDomain)
	require.Equal(t, before.Height, after.Height)
	require.Equal(t, before.Timestamp, after.Timestamp)
	require.Equal(t, before.LcStoreCommit, after.LcStoreCommit)

	previous, err = ism.SetIdentities("celestia1owner", []*types.EnclaveIdentity{next})
	require.NoError(t, err)
	require.Equal(t, both, previous)
	final, err := types.DecodeIsmState(ism.State)
	require.NoError(t, err)
	require.Equal(t, next.Digest(), final.IdentityDigest)
}

func TestSettingAnUnusableSetIsRefused(t *testing.T) {
	ism := ismPinnedTo(identityFor(1))
	before := append([]byte(nil), ism.State...)
	bad := identityFor(2)
	bad.MrTd = bad.MrTd[:10]
	_, err := ism.SetIdentities("celestia1owner", []*types.EnclaveIdentity{bad})
	require.Error(t, err)
	_, err = ism.SetIdentities("celestia1owner", nil)
	require.Error(t, err)
	_, err = ism.SetIdentities("celestia1owner", []*types.EnclaveIdentity{identityFor(3), identityFor(3)})
	require.Error(t, err)
	require.Equal(t, before, ism.State)
}
