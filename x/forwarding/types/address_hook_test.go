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

// Binding the hook is only meaningful if it actually changes the address: a hook-bound
// address must differ from the default-hook address for the same destination, and two
// different hooks must yield two different addresses.
func TestDeriveForwardingAddressWithHookIsDistinct(t *testing.T) {
	destRecipient := make([]byte, types.RecipientLength)
	tokenID := tokenIDBytes(t, 7)

	base, err := types.DeriveForwardingAddress(42161, destRecipient, tokenID)
	require.NoError(t, err)

	hookA, err := types.DeriveForwardingAddressWithHook(42161, destRecipient, tokenID, hookIDBytes(t, 1), nil)
	require.NoError(t, err)

	hookB, err := types.DeriveForwardingAddressWithHook(42161, destRecipient, tokenID, hookIDBytes(t, 2), nil)
	require.NoError(t, err)

	require.NotEqual(t, base, hookA, "hook-bound address must differ from the default-hook address")
	require.NotEqual(t, hookA, hookB, "different hooks must derive different addresses")
	require.Len(t, hookA, types.CosmosAddressLen)
}

// Pins the exact preimage so a client (bot.fun, the relayer) can reimplement it and any
// accidental change to the scheme shows up as a test failure rather than lost deposits.
func TestDeriveForwardingAddressWithHookIntermediates(t *testing.T) {
	destDomain := uint32(42161)
	destRecipient := make([]byte, types.RecipientLength)
	destRecipient[31] = 0xEF
	tokenID := tokenIDBytes(t, 3)
	hookID := hookIDBytes(t, 5)

	destDomainBytes := make([]byte, types.DomainEncodingSize)
	binary.BigEndian.PutUint32(destDomainBytes[types.DomainOffset:], destDomain)

	h := sha256.New()
	h.Write(destDomainBytes)
	h.Write(destRecipient)
	h.Write(tokenID)
	h.Write(hookID)
	callDigest := h.Sum(nil)

	h.Reset()
	h.Write([]byte{types.ForwardVersionHook})
	h.Write(callDigest)
	salt := h.Sum(nil)

	want := address.Module(types.ModuleName, salt)[:types.CosmosAddressLen]

	got, err := types.DeriveForwardingAddressWithHook(destDomain, destRecipient, tokenID, hookID, nil)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestDeriveForwardingAddressWithHookRejectsBadHookID(t *testing.T) {
	destRecipient := make([]byte, types.RecipientLength)
	tokenID := tokenIDBytes(t, 1)

	testCases := []struct {
		name   string
		hookID []byte
	}{
		{"too_short", make([]byte, types.HookIDLength-1)},
		{"too_long", make([]byte, types.HookIDLength+1)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Metadata is set so the rejection is unambiguously about the hook length
			// rather than the "nothing committed" case.
			_, err := types.DeriveForwardingAddressWithHook(1, destRecipient, tokenID, tc.hookID, []byte{0xab})
			require.ErrorIs(t, err, types.ErrInvalidHookID)
		})
	}
}

// Recipient and token validation must apply to the hook-bound scheme too.
func TestDeriveForwardingAddressWithHookValidatesOtherFields(t *testing.T) {
	hookID := hookIDBytes(t, 1)

	_, err := types.DeriveForwardingAddressWithHook(1, make([]byte, 31), tokenIDBytes(t, 1), hookID, nil)
	require.ErrorIs(t, err, types.ErrInvalidRecipient)

	_, err = types.DeriveForwardingAddressWithHook(1, make([]byte, types.RecipientLength), make([]byte, 31), hookID, nil)
	require.ErrorIs(t, err, types.ErrInvalidTokenID)
}

// Metadata is part of the commitment, so it must move the address independently of the
// hook, and a metadata-only binding must be reachable (hook absent => mailbox default).
func TestDeriveForwardingAddressWithHookCommitsMetadata(t *testing.T) {
	destRecipient := make([]byte, types.RecipientLength)
	tokenID := tokenIDBytes(t, 7)
	hookID := hookIDBytes(t, 1)

	base, err := types.DeriveForwardingAddress(42161, destRecipient, tokenID)
	require.NoError(t, err)

	hookOnly, err := types.DeriveForwardingAddressWithHook(42161, destRecipient, tokenID, hookID, nil)
	require.NoError(t, err)

	hookMetaA, err := types.DeriveForwardingAddressWithHook(42161, destRecipient, tokenID, hookID, []byte{0xab, 0xcd})
	require.NoError(t, err)

	hookMetaB, err := types.DeriveForwardingAddressWithHook(42161, destRecipient, tokenID, hookID, []byte{0xde, 0xad})
	require.NoError(t, err)

	metaOnly, err := types.DeriveForwardingAddressWithHook(42161, destRecipient, tokenID, nil, []byte{0xab, 0xcd})
	require.NoError(t, err)

	require.NotEqual(t, hookOnly, hookMetaA, "adding metadata must change the address")
	require.NotEqual(t, hookMetaA, hookMetaB, "different metadata must derive different addresses")
	require.NotEqual(t, base, metaOnly, "a metadata-only binding must differ from the default address")
	require.NotEqual(t, hookMetaA, metaOnly, "same metadata under a different hook must differ")

	// An absent hook normalises to the zero address, so passing it explicitly is equivalent.
	explicitZero, err := types.DeriveForwardingAddressWithHook(42161, destRecipient, tokenID, make([]byte, types.HookIDLength), []byte{0xab, 0xcd})
	require.NoError(t, err)
	require.Equal(t, metaOnly, explicitZero)
}

// Committing nothing is not a binding: that is what DeriveForwardingAddress is for.
func TestDeriveForwardingAddressWithHookRequiresSomething(t *testing.T) {
	destRecipient := make([]byte, types.RecipientLength)
	tokenID := tokenIDBytes(t, 1)

	for _, tc := range []struct {
		name   string
		hookID []byte
	}{
		{"nil_hook", nil},
		{"empty_hook", []byte{}},
		{"zero_hook", make([]byte, types.HookIDLength)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := types.DeriveForwardingAddressWithHook(1, destRecipient, tokenID, tc.hookID, nil)
			require.ErrorIs(t, err, types.ErrInvalidHookID)
		})
	}
}
