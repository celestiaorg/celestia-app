package types_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v10/x/teeism/types"
)

func identityFor(b byte) *types.EnclaveIdentity {
	fill := func(n int) []byte { return bytes.Repeat([]byte{b}, n) }
	return &types.EnclaveIdentity{
		MrTd:        fill(types.MrTdLen),
		OsImageHash: fill(32),
		ComposeHash: fill(32),
		MrKms:       fill(32),
		KeyProvider: fill(16),
	}
}

func ismPinnedTo(id *types.EnclaveIdentity) types.InterchainSecurityModule {
	state := &types.IsmState{
		OriginDomain:   7,
		Height:         100,
		Timestamp:      1000,
		IdentityDigest: id.Digest(),
	}
	state.StateRoot[0] = 1
	state.LcStoreCommit[0] = 2
	return types.InterchainSecurityModule{Owner: "celestia1owner", State: types.EncodeIsmState(state), Identity: id}
}

func TestOnlyTheOwnerRepins(t *testing.T) {
	old, next := identityFor(1), identityFor(2)
	ism := ismPinnedTo(old)
	before := append([]byte(nil), ism.State...)

	_, err := ism.Repin("celestia1someoneelse", next)
	require.Error(t, err)
	_, err = ism.Repin("", next)
	require.Error(t, err)
	require.Equal(t, before, ism.State, "a refused repin changes nothing")
	require.Equal(t, old, ism.Identity)

	unowned := ismPinnedTo(old)
	unowned.Owner = ""
	_, err = unowned.Repin("", next)
	require.Error(t, err, "an ism without an owner cannot be repinned by anyone")
}

func TestARepinSwapsOnlyTheIdentity(t *testing.T) {
	old, next := identityFor(1), identityFor(2)
	ism := ismPinnedTo(old)
	before, err := types.DecodeIsmState(ism.State)
	require.NoError(t, err)

	previous, err := ism.Repin("celestia1owner", next)
	require.NoError(t, err)
	require.Equal(t, old.Digest(), previous)
	require.Equal(t, next, ism.Identity)

	after, err := types.DecodeIsmState(ism.State)
	require.NoError(t, err)
	require.Equal(t, next.Digest(), after.IdentityDigest)
	// Everything the light client and the transition rules depend on is untouched.
	require.Equal(t, before.StateRoot, after.StateRoot)
	require.Equal(t, before.OriginDomain, after.OriginDomain)
	require.Equal(t, before.Height, after.Height)
	require.Equal(t, before.Timestamp, after.Timestamp)
	require.Equal(t, before.LcStoreCommit, after.LcStoreCommit)
}

func TestARepinToAnUnusableIdentityIsRefused(t *testing.T) {
	ism := ismPinnedTo(identityFor(1))
	bad := identityFor(2)
	bad.MrTd = bad.MrTd[:10]
	_, err := ism.Repin("celestia1owner", bad)
	require.Error(t, err)
	_, err = ism.Repin("celestia1owner", nil)
	require.Error(t, err)
}
