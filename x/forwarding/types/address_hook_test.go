package types_test

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/x/forwarding/types"
	"github.com/cosmos/cosmos-sdk/types/address"
	"github.com/stretchr/testify/require"
)

func hookIDBytes(t *testing.T, id byte) []byte {
	t.Helper()
	h := make([]byte, types.HookIDLength)
	h[types.HookIDLength-1] = id
	return h
}

// Every distinct (hook, metadata) pair must land on a distinct address, and none may
// collide with the default-hook address.
func TestDeriveForwardingAddressWithHookIsDistinct(t *testing.T) {
	recipient := make([]byte, types.RecipientLength)
	tokenID := tokenIDBytes(t, 7)
	hookA, hookB := hookIDBytes(t, 1), hookIDBytes(t, 2)
	metaA, metaB := []byte{0xab, 0xcd}, []byte{0xde, 0xad}

	derive := func(hook, meta []byte) []byte {
		addr, err := types.DeriveForwardingAddressWithHook(42161, recipient, tokenID, hook, meta)
		require.NoError(t, err)
		require.Len(t, addr, types.CosmosAddressLen)
		return addr
	}

	base, err := types.DeriveForwardingAddress(42161, recipient, tokenID)
	require.NoError(t, err)

	variants := map[string][]byte{
		"hookA":             derive(hookA, nil),
		"hookB":             derive(hookB, nil),
		"hookA+metaA":       derive(hookA, metaA),
		"hookA+metaB":       derive(hookA, metaB),
		"hookB+metaA":       derive(hookB, metaA),
		"metaA only":        derive(nil, metaA),
		"default (no bind)": base,
	}

	for nameX, x := range variants {
		for nameY, y := range variants {
			if nameX < nameY {
				require.NotEqual(t, x, y, "%s and %s must derive different addresses", nameX, nameY)
			}
		}
	}

	// An absent hook normalises to the zero address, so passing it explicitly is equivalent.
	require.Equal(t, variants["metaA only"], derive(make([]byte, types.HookIDLength), metaA))
}

// Pins the exact preimage so clients can reimplement it and a change to the scheme fails
// here rather than sending deposits to an unforwardable address.
func TestDeriveForwardingAddressWithHookIntermediates(t *testing.T) {
	destDomain := uint32(42161)
	destRecipient := make([]byte, types.RecipientLength)
	destRecipient[31] = 0xEF
	tokenID := tokenIDBytes(t, 3)
	hookID := hookIDBytes(t, 5)
	metadata := []byte{0xab, 0xcd}

	destDomainBytes := make([]byte, types.DomainEncodingSize)
	binary.BigEndian.PutUint32(destDomainBytes[types.DomainOffset:], destDomain)

	h := sha256.New()
	h.Write(destDomainBytes)
	h.Write(destRecipient)
	h.Write(tokenID)
	h.Write(hookID)
	h.Write(metadata)
	callDigest := h.Sum(nil)

	h.Reset()
	h.Write([]byte{types.ForwardVersionHook})
	h.Write(callDigest)
	salt := h.Sum(nil)

	want := address.Module(types.ModuleName, salt)[:types.CosmosAddressLen]

	got, err := types.DeriveForwardingAddressWithHook(destDomain, destRecipient, tokenID, hookID, metadata)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestDeriveForwardingAddressWithHookRejects(t *testing.T) {
	recipient := make([]byte, types.RecipientLength)
	tokenID := tokenIDBytes(t, 1)

	// A 20-byte cosmos address is the likely mistake here, hence the length guard.
	_, err := types.DeriveForwardingAddressWithHook(1, recipient, tokenID, make([]byte, 20), []byte{0xab})
	require.ErrorIs(t, err, types.ErrInvalidHookID)

	// A zero hook means the mailbox default, so on its own it commits to nothing.
	_, err = types.DeriveForwardingAddressWithHook(1, recipient, tokenID, make([]byte, types.HookIDLength), nil)
	require.ErrorIs(t, err, types.ErrInvalidHookID)
}
