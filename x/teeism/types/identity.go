package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	errorsmod "cosmossdk.io/errors"
)

// identityDomainTag separates this digest from every other sha256 in the system.
// v3 adds RTMR0-2. v2 was the dstack 0.6 identity without them; v1 also pinned
// dstack 0.5's mr-kms event, which 0.6 no longer records.
const identityDomainTag = "tee-isms/enclave-identity/v3"

// MrTdLen is the length of the TD build-time measurement.
const MrTdLen = 48

// pinnedRtmrs are the runtime registers the boot chain extends, in register
// order. RTMR3 is not among them: the pinned events govern it.
func (id *EnclaveIdentity) pinnedRtmrs() [ApplicationIMR][]byte {
	return [ApplicationIMR][]byte{id.Rtmr0, id.Rtmr1, id.Rtmr2}
}

// pinnedEvents pairs each pinned event-log key with the value this identity
// requires. The order is fixed because it is hashed. The KMS is pinned through
// key-provider, which names the KMS's key.
func (id *EnclaveIdentity) pinnedEvents() [3]struct {
	Name  string
	Value []byte
} {
	return [3]struct {
		Name  string
		Value []byte
	}{
		{EventOsImageHash, id.OsImageHash},
		{EventComposeHash, id.ComposeHash},
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
	for _, rtmr := range id.pinnedRtmrs() {
		h.Write(rtmr)
	}
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
	for i, rtmr := range id.pinnedRtmrs() {
		if len(rtmr) != DigestLen {
			return ErrInvalidEnclaveIdentity.Wrapf("rtmr%d must be %d bytes, got %d", i, DigestLen, len(rtmr))
		}
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
	if err := id.verifyMeasurements(measured); err != nil {
		return err
	}
	events, err := IndexApplicationEvents(eventlog)
	if err != nil {
		return ErrIdentityMismatch.Wrap(err.Error())
	}
	return id.verifyEvents(events)
}

// verifyMeasurements is the quote half of VerifyEnclaveIdentity: the firmware
// and the boot chain, which the hardware signed directly.
func (id *EnclaveIdentity) verifyMeasurements(measured *Measurements) error {
	if !bytes.Equal(id.MrTd, measured.MrTd[:]) {
		return ErrIdentityMismatch.Wrapf("mr_td does not match the pinned platform measurement: pinned %x, quoted %x",
			id.MrTd, measured.MrTd[:])
	}
	// MRTD covers only the firmware. The kernel, its command line and the VM
	// configuration land in RTMR0-2, so an unpinned boot chain could replay the
	// pinned events into RTMR3 itself.
	for i, rtmr := range id.pinnedRtmrs() {
		if !bytes.Equal(rtmr, measured.Rtmrs[i][:]) {
			return ErrIdentityMismatch.Wrapf("rtmr%d does not match the pinned boot chain: pinned %x, quoted %x",
				i, rtmr, measured.Rtmrs[i][:])
		}
	}
	return nil
}

// verifyEvents is the pinned-events half of VerifyEnclaveIdentity, on a log
// already indexed, so a set of identities shares one pass over the log.
func (id *EnclaveIdentity) verifyEvents(events *ApplicationEvents) error {
	for _, ev := range id.pinnedEvents() {
		got, ok := events.Get(ev.Name)
		if !ok {
			return ErrIdentityMismatch.Wrapf("event %q is missing, duplicated, or its digest does not commit to its text", ev.Name)
		}
		if !bytes.Equal(got, ev.Value) {
			return ErrIdentityMismatch.Wrapf("event %q does not match the pinned value: pinned %x, quoted %x", ev.Name, ev.Value, got)
		}
	}
	return nil
}

// VerifyEnclaveIdentities accepts a quote from any one of the pinned enclaves.
//
// The event log is checked against the quote once, before any identity is tried:
// without that the log proves nothing, and no identity may be matched against it.
// The first identity that matches wins. If none does, the error names each pinned
// identity by the start of its digest with the reason it did not match, so a
// wrong pin can be told from a wrong enclave.
func VerifyEnclaveIdentities(
	ids []*EnclaveIdentity,
	measured *Measurements,
	eventlog []EventLog,
	replayedRtmrs [RtmrCount]Digest,
) error {
	if len(ids) == 0 {
		return ErrInvalidEnclaveIdentity.Wrap("no pinned enclave identity")
	}
	if replayedRtmrs != measured.Rtmrs {
		return ErrIdentityMismatch.Wrap("event log does not replay to the RTMRs in the quote")
	}
	// One pass over the log for every identity: the self-commitment checks and
	// the name rules do not depend on which identity is being tried.
	events, err := IndexApplicationEvents(eventlog)
	if err != nil {
		return ErrIdentityMismatch.Wrap(err.Error())
	}
	reasons := make([]string, 0, len(ids))
	for _, id := range ids {
		err := id.verifyMeasurements(measured)
		if err == nil {
			err = id.verifyEvents(events)
		}
		if err == nil {
			return nil
		}
		d := id.Digest()
		reasons = append(reasons, fmt.Sprintf("%x: %v", d[:4], err))
	}
	return ErrIdentityMismatch.Wrapf("none of the %d pinned enclaves matches: %s", len(ids), strings.Join(reasons, "; "))
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

// MaxEnclaveIdentities bounds how many enclaves one ism may pin. Each pinned
// identity is tried against every attestation, so the set stays small; a handful
// covers redundancy and a rotation in progress.
const MaxEnclaveIdentities = 8

// identitySetDomainTag separates a set digest from every other sha256 in the
// system. The EVM ISM (TeeDcapIsm) computes the same bytes from the same sorted
// per-identity digests, so the two chains name the same set the same way.
const identitySetDomainTag = "teeism/identity-set/v1"

// IdentitySetDigest is the digest of a set of pinned identities, which an ism's
// state carries in place of a single identity digest.
//
// A set of one has the digest of its only member, so an ism pinning one enclave
// carries exactly what it would without sets. Two or more hash as
//
//	sha256("teeism/identity-set/v1" || uint8(n) || each identity digest, sorted ascending)
//
// so the order the set was given in never matters. The set must be 1 to
// MaxEnclaveIdentities identities, each valid, no two with the same digest.
func IdentitySetDigest(ids []*EnclaveIdentity) ([32]byte, error) {
	digests, err := identityDigests(ids)
	if err != nil {
		return [32]byte{}, err
	}
	return DigestOfSortedDigests(digests), nil
}

// identityDigests validates a set and returns each member's digest, sorted
// ascending.
func identityDigests(ids []*EnclaveIdentity) ([][32]byte, error) {
	if len(ids) == 0 {
		return nil, ErrInvalidEnclaveIdentity.Wrap("at least one enclave identity is required")
	}
	if len(ids) > MaxEnclaveIdentities {
		return nil, ErrInvalidEnclaveIdentity.Wrapf("at most %d enclave identities, got %d", MaxEnclaveIdentities, len(ids))
	}
	digests := make([][32]byte, len(ids))
	for i, id := range ids {
		if err := id.Validate(); err != nil {
			return nil, errorsmod.Wrapf(err, "identity %d", i)
		}
		digests[i] = id.Digest()
	}
	sort.Slice(digests, func(i, j int) bool { return bytes.Compare(digests[i][:], digests[j][:]) < 0 })
	for i := 1; i < len(digests); i++ {
		if digests[i] == digests[i-1] {
			return nil, ErrInvalidEnclaveIdentity.Wrapf("identity %x is pinned twice", digests[i][:4])
		}
	}
	return digests, nil
}

// DigestOfSortedDigests hashes already validated, sorted, distinct identity
// digests. Exported so the vectors the EVM ISM shares can be checked directly.
func DigestOfSortedDigests(sorted [][32]byte) [32]byte {
	if len(sorted) == 1 {
		return sorted[0]
	}
	h := sha256.New()
	h.Write([]byte(identitySetDomainTag))
	h.Write([]byte{byte(len(sorted))})
	for _, d := range sorted {
		h.Write(d[:])
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// SortIdentities returns the set in canonical order, ascending by digest, which is
// how an ism stores it. It validates the set the same way IdentitySetDigest does.
func SortIdentities(ids []*EnclaveIdentity) ([]*EnclaveIdentity, error) {
	if _, err := identityDigests(ids); err != nil {
		return nil, err
	}
	out := append([]*EnclaveIdentity(nil), ids...)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Digest(), out[j].Digest()
		return bytes.Compare(a[:], b[:]) < 0
	})
	return out, nil
}
