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

// The binding is only meaningful if every distinct (hook, metadata) pair lands on a
// distinct address, and if none of them collide with the default-hook address.
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

// Pins the exact preimage so a client (bot.fun, the relayer) can reimplement it and any
// accidental change to the scheme shows up as a test failure rather than lost deposits.
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
	hookID := hookIDBytes(t, 1)

	testCases := []struct {
		name      string
		recipient []byte
		tokenID   []byte
		hookID    []byte
		metadata  []byte
		wantErr   error
	}{
		// Committing to nothing is not a binding; DeriveForwardingAddress is that call.
		{"nil hook, no metadata", recipient, tokenID, nil, nil, types.ErrInvalidHookID},
		{"empty hook, no metadata", recipient, tokenID, []byte{}, nil, types.ErrInvalidHookID},
		{"zero hook, no metadata", recipient, tokenID, make([]byte, types.HookIDLength), nil, types.ErrInvalidHookID},
		// Metadata is set in the length cases so the rejection is unambiguously about length.
		{"hook too short", recipient, tokenID, make([]byte, types.HookIDLength-1), []byte{0xab}, types.ErrInvalidHookID},
		{"hook too long", recipient, tokenID, make([]byte, types.HookIDLength+1), []byte{0xab}, types.ErrInvalidHookID},
		// Recipient and token validation must apply to the bound scheme too.
		{"bad recipient", make([]byte, 31), tokenID, hookID, nil, types.ErrInvalidRecipient},
		{"bad token id", recipient, make([]byte, 31), hookID, nil, types.ErrInvalidTokenID},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := types.DeriveForwardingAddressWithHook(1, tc.recipient, tc.tokenID, tc.hookID, tc.metadata)
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
