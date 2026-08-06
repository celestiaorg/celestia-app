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

	hookA, err := types.DeriveForwardingAddressWithHook(42161, destRecipient, tokenID, hookIDBytes(t, 1))
	require.NoError(t, err)

	hookB, err := types.DeriveForwardingAddressWithHook(42161, destRecipient, tokenID, hookIDBytes(t, 2))
	require.NoError(t, err)

	require.NotEqual(t, base, hookA, "hook-bound address must differ from the default-hook address")
	require.NotEqual(t, hookA, hookB, "different hooks must derive different addresses")
	require.Len(t, hookA, types.CosmosAddressLen)
}

func TestDeriveForwardingAddressWithHookIsDeterministic(t *testing.T) {
	destRecipient := make([]byte, types.RecipientLength)
	tokenID := tokenIDBytes(t, 7)
	hookID := hookIDBytes(t, 9)

	first, err := types.DeriveForwardingAddressWithHook(1, destRecipient, tokenID, hookID)
	require.NoError(t, err)
	second, err := types.DeriveForwardingAddressWithHook(1, destRecipient, tokenID, hookID)
	require.NoError(t, err)

	require.Equal(t, first, second)
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

	got, err := types.DeriveForwardingAddressWithHook(destDomain, destRecipient, tokenID, hookID)
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
		{"empty", []byte{}},
		{"too_short", make([]byte, types.HookIDLength-1)},
		{"too_long", make([]byte, types.HookIDLength+1)},
		// The zero hook id is the sentinel for "mailbox default hook"; binding to it
		// would create a second address for a destination that already has one.
		{"zero_hook", make([]byte, types.HookIDLength)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := types.DeriveForwardingAddressWithHook(1, destRecipient, tokenID, tc.hookID)
			require.ErrorIs(t, err, types.ErrInvalidHookID)
		})
	}
}

// Recipient and token validation must apply to the hook-bound scheme too.
func TestDeriveForwardingAddressWithHookValidatesOtherFields(t *testing.T) {
	hookID := hookIDBytes(t, 1)

	_, err := types.DeriveForwardingAddressWithHook(1, make([]byte, 31), tokenIDBytes(t, 1), hookID)
	require.ErrorIs(t, err, types.ErrInvalidRecipient)

	_, err = types.DeriveForwardingAddressWithHook(1, make([]byte, types.RecipientLength), make([]byte, 31), hookID)
	require.ErrorIs(t, err, types.ErrInvalidTokenID)
}
