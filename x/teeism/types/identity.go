package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// identityDomainTag separates this digest from every other sha256 in the system.
// It matches the tag the enclave and its circuits use, so an identity digest
// computed here is the same 32 bytes the enclave writes into its ISM state.
const identityDomainTag = "tee-isms/enclave-identity/v1"

// MrTdLen is the length of the TD build-time measurement.
const MrTdLen = 48

// pinnedEvents pairs each pinned event-log key with the value this identity
// requires. The order is fixed because it is hashed.
func (id *EnclaveIdentity) pinnedEvents() [4]struct {
	Name  string
	Value []byte
} {
	return [4]struct {
		Name  string
		Value []byte
	}{
		{EventOsImageHash, id.OsImageHash},
		{EventComposeHash, id.ComposeHash},
		{EventMrKms, id.MrKms},
		{EventKeyProvider, id.KeyProvider},
	}
}

// Digest is a stable id for a pinned identity, mirrored into the ISM state so
// anyone can see which enclave an ISM was created for without disassembling an
// image.
func (id *EnclaveIdentity) Digest() [32]byte {
	h := sha256.New()
	h.Write([]byte(identityDomainTag))
	h.Write(id.MrTd)
	for _, ev := range id.pinnedEvents() {
		_ = binary.Write(h, binary.BigEndian, uint64(len(ev.Name)))
		h.Write([]byte(ev.Name))
		_ = binary.Write(h, binary.BigEndian, uint64(len(ev.Value)))
		h.Write(ev.Value)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Validate rejects an identity that could never match a real quote.
func (id *EnclaveIdentity) Validate() error {
	if id == nil {
		return ErrInvalidEnclaveIdentity.Wrap("identity is required")
	}
	if len(id.MrTd) != MrTdLen {
		return ErrInvalidEnclaveIdentity.Wrapf("mr_td must be %d bytes, got %d", MrTdLen, len(id.MrTd))
	}
	for _, ev := range id.pinnedEvents() {
		if len(ev.Value) == 0 {
			return ErrInvalidEnclaveIdentity.Wrapf("%s must not be empty", ev.Name)
		}
	}
	return nil
}

// VerifyEnclaveIdentity is this module's verdict on the software stack, as
// distinct from Intel's verdict on the hardware.
//
// replayedRtmrs is the caller's replay of eventlog, passed in so it happens once.
// Checking it against the quote is what makes the event log believable at all;
// without it the log is untrusted text sitting next to a signature.
func VerifyEnclaveIdentity(
	id *EnclaveIdentity,
	measured *Measurements,
	eventlog []EventLog,
	replayedRtmrs [RtmrCount]Digest,
) error {
	if replayedRtmrs != measured.Rtmrs {
		return ErrIdentityMismatch.Wrap("event log does not replay to the RTMRs in the quote")
	}
	if !bytes.Equal(id.MrTd, measured.MrTd[:]) {
		return ErrIdentityMismatch.Wrapf("mr_td does not match the pinned platform measurement: pinned %x, quoted %x",
			id.MrTd, measured.MrTd[:])
	}
	for _, ev := range id.pinnedEvents() {
		got, ok := GetEventValue(eventlog, ev.Name)
		if !ok {
			return ErrIdentityMismatch.Wrapf("event %q is missing, duplicated, or its digest does not commit to its text", ev.Name)
		}
		if !bytes.Equal(got, ev.Value) {
			return ErrIdentityMismatch.Wrapf("event %q does not match the pinned value: pinned %x, quoted %x", ev.Name, ev.Value, got)
		}
	}
	return nil
}

// Measurements are the fields of a TDX report a relying party may pin.
type Measurements struct {
	MrTd  [DigestLen]byte
	Rtmrs [RtmrCount]Digest
}

// MeasurementsFrom assembles the pinnable fields from a decoded TD quote body.
func MeasurementsFrom(mrTd []byte, rtmrs [][]byte) (*Measurements, error) {
	if len(mrTd) != DigestLen {
		return nil, fmt.Errorf("mr_td must be %d bytes, got %d", DigestLen, len(mrTd))
	}
	if len(rtmrs) != RtmrCount {
		return nil, fmt.Errorf("expected %d rtmrs, got %d", RtmrCount, len(rtmrs))
	}
	m := &Measurements{}
	copy(m.MrTd[:], mrTd)
	for i, r := range rtmrs {
		if len(r) != DigestLen {
			return nil, fmt.Errorf("rtmr%d must be %d bytes, got %d", i, DigestLen, len(r))
		}
		copy(m.Rtmrs[i][:], r)
	}
	return m, nil
}
