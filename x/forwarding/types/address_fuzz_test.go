package types_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/forwarding/types"
)

// fuzzDerivation is one DeriveForwardingAddress input decoded from fuzz bytes.
type fuzzDerivation struct {
	domain    uint32
	recipient []byte
	tokenID   []byte
	hookID    []byte // nil, all-zero or 32 arbitrary bytes
	metadata  []byte
}

// parseDerivation reads domain(4) || recipient(32) || tokenID(32) || hookSel(1)
// [|| hookID(32)] || metadata, zero-padding a short input.
func parseDerivation(b []byte) fuzzDerivation {
	const fixed = 4 + types.RecipientLength + types.TokenIDLength + 1
	head := make([]byte, fixed)
	copy(head, b)
	rest := b[min(len(b), fixed):]
	d := fuzzDerivation{
		domain:    binary.BigEndian.Uint32(head),
		recipient: head[4 : 4+types.RecipientLength],
		tokenID:   head[4+types.RecipientLength : 4+types.RecipientLength+types.TokenIDLength],
	}
	switch head[fixed-1] % 3 {
	case 1:
		d.hookID = make([]byte, types.HookIDLength)
	case 2:
		d.hookID = make([]byte, types.HookIDLength)
		n := copy(d.hookID, rest)
		rest = rest[n:]
	}
	d.metadata = rest
	return d
}

// sameBinding reports whether two derivations denote the same forwarding
// commitment: a nil and a zero hook both mean the mailbox default hook, and
// nil and empty metadata are the same.
func sameBinding(a, b fuzzDerivation) bool {
	hook := func(h []byte) []byte {
		if bytes.Equal(h, make([]byte, types.HookIDLength)) {
			return nil
		}
		return h
	}
	return a.domain == b.domain &&
		bytes.Equal(a.recipient, b.recipient) &&
		bytes.Equal(a.tokenID, b.tokenID) &&
		bytes.Equal(hook(a.hookID), hook(b.hookID)) &&
		bytes.Equal(a.metadata, b.metadata)
}

func derive(t *testing.T, d fuzzDerivation) []byte {
	t.Helper()
	addr, err := types.DeriveForwardingAddress(d.domain, d.recipient, d.tokenID, d.hookID, d.metadata)
	if err != nil {
		t.Fatalf("valid lengths rejected: %v", err)
	}
	if len(addr) != types.CosmosAddressLen {
		t.Fatalf("address has %d bytes", len(addr))
	}
	return addr
}

// FuzzDeriveForwardingAddress checks that the forwarding address, which is the
// only authorization MsgForward has, is injective: two inputs derive the same
// address iff they denote the same (destination domain, recipient, token, hook,
// metadata) commitment. It also checks the length validation of each field.
func FuzzDeriveForwardingAddress(f *testing.F) {
	base := make([]byte, 4+types.RecipientLength+types.TokenIDLength+1)
	base[3] = 1
	base[35] = 0xaa
	base[67] = 0x01
	with := func(hookSel byte, tail ...byte) []byte {
		b := append([]byte(nil), base...)
		b[len(b)-1] = hookSel
		return append(b, tail...)
	}
	hook := bytes.Repeat([]byte{0xab}, types.HookIDLength)
	// Equivalent bindings: no hook, zero hook, empty metadata.
	f.Add(with(0), with(1), byte(32))
	// A hook-only binding versus a metadata-only binding of the same bytes.
	f.Add(with(2, hook...), with(0, hook...), byte(32))
	f.Add(with(2, append(hook, 1, 2)...), with(1, append(hook, 1, 2)...), byte(31))
	// Metadata moved across the hook boundary.
	f.Add(with(2, append(hook, 7)...), with(2, append(append([]byte{7}, hook[1:]...), 0xab)...), byte(33))
	f.Add(with(0, 0), with(0), byte(0))
	f.Add([]byte{}, []byte{0, 0, 0, 1}, byte(20))

	f.Fuzz(func(t *testing.T, a, b []byte, recipientLen byte) {
		da, db := parseDerivation(a), parseDerivation(b)
		addrA, addrB := derive(t, da), derive(t, db)
		if !bytes.Equal(addrA, derive(t, da)) {
			t.Fatal("derivation is not deterministic")
		}
		if same := sameBinding(da, db); same != bytes.Equal(addrA, addrB) {
			t.Fatalf("same binding=%t but equal addresses=%t", same, !same)
		}

		// Field lengths: recipient and token must be 32 bytes, a hook 0 or 32.
		n := int(recipientLen) % 40
		short := append(append([]byte(nil), b...), make([]byte, 40)...)[:n]
		_, errRecipient := types.DeriveForwardingAddress(da.domain, short, da.tokenID, nil, nil)
		_, errToken := types.DeriveForwardingAddress(da.domain, da.recipient, short, nil, nil)
		_, errHook := types.DeriveForwardingAddress(da.domain, da.recipient, da.tokenID, short, nil)
		if (errRecipient == nil) != (n == types.RecipientLength) || (errToken == nil) != (n == types.TokenIDLength) {
			t.Fatalf("length %d: recipient err %v, token err %v", n, errRecipient, errToken)
		}
		if (errHook == nil) != (n == 0 || n == types.HookIDLength) {
			t.Fatalf("hook length %d: err %v", n, errHook)
		}
	})
}
