package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// The byte formats in this file are not ours to choose independently: the enclave
// encodes them in Rust and this module decodes them in Go, so a single byte of
// drift rejects every attestation the bridge submits. They are pinned by golden
// tests against vectors produced by the Rust encoder.

const (
	// IsmStateBytes is the exact length of an encoded ISM state.
	//
	// Only the first 32 bytes have a meaning imposed from outside: Hyperlane
	// treats them as the origin state root. Everything past the root is ours to
	// define, which is where the light client commitment and the enclave
	// identity digest live.
	IsmStateBytes = 116

	// MinStateBytes is the minimum size of a trusted ism state.
	MinStateBytes = 32
	// MaxStateBytes is the maximum size of a trusted ism state (2 KiB).
	MaxStateBytes = 2048

	// MaxMessageIDsCount caps the number of message ids in one attested batch.
	MaxMessageIDsCount = 1_000_000

	offOriginDomain   = 32
	offHeight         = 36
	offTimestamp      = 44
	offLcStoreCommit  = 52
	offIdentityDigest = 84

	// attestedUpdateHead is everything in an attested update before the message
	// ids: two states, a 32-byte tree address, an 8-byte timestamp and an 8-byte
	// count.
	attestedUpdateHead = 2*IsmStateBytes + 48
)

// IsmState is the trusted state an ISM carries between attestations.
//
// All integer fields are big-endian.
type IsmState struct {
	// StateRoot is the origin app hash (Celestia) or execution state root (EVM).
	StateRoot [32]byte
	// OriginDomain is the Hyperlane domain of the origin chain. Fixed at ISM
	// creation and carried unchanged forever, which is what stops an attestation
	// for one origin being replayed into an ISM built for another.
	OriginDomain uint32
	// Height is the origin block height, or execution block number.
	Height uint64
	// Timestamp is the origin head time in seconds, non-decreasing across the
	// state chain.
	Timestamp uint64
	// LcStoreCommit is a sha256 commitment to the light client store the enclave
	// was handed. The destination chain is the light client's authenticated
	// database, which is what lets the enclave stay stateless.
	LcStoreCommit [32]byte
	// IdentityDigest names which enclave may advance this ISM.
	IdentityDigest [32]byte
}

// EncodeIsmState serializes a state to its canonical 116-byte form.
func EncodeIsmState(s *IsmState) []byte {
	out := make([]byte, IsmStateBytes)
	copy(out[:offOriginDomain], s.StateRoot[:])
	binary.BigEndian.PutUint32(out[offOriginDomain:offHeight], s.OriginDomain)
	binary.BigEndian.PutUint64(out[offHeight:offTimestamp], s.Height)
	binary.BigEndian.PutUint64(out[offTimestamp:offLcStoreCommit], s.Timestamp)
	copy(out[offLcStoreCommit:offIdentityDigest], s.LcStoreCommit[:])
	copy(out[offIdentityDigest:], s.IdentityDigest[:])
	return out
}

// DecodeIsmState parses a canonical state, rejecting any other length.
func DecodeIsmState(b []byte) (*IsmState, error) {
	if len(b) != IsmStateBytes {
		return nil, fmt.Errorf("ism state must be %d bytes, got %d", IsmStateBytes, len(b))
	}
	s := &IsmState{
		OriginDomain: binary.BigEndian.Uint32(b[offOriginDomain:offHeight]),
		Height:       binary.BigEndian.Uint64(b[offHeight:offTimestamp]),
		Timestamp:    binary.BigEndian.Uint64(b[offTimestamp:offLcStoreCommit]),
	}
	copy(s.StateRoot[:], b[:offOriginDomain])
	copy(s.LcStoreCommit[:], b[offLcStoreCommit:offIdentityDigest])
	copy(s.IdentityDigest[:], b[offIdentityDigest:])
	return s, nil
}

// VerifyTransition enforces the rules every attested transition must satisfy.
//
// These are deliberately checked on-chain rather than taken on the enclave's
// word. The enclave checks them too, but an ISM that trusts its enclave to be
// correct as well as honest has no defence against an enclave bug.
func VerifyTransition(prev, next *IsmState) error {
	if prev.OriginDomain != next.OriginDomain {
		return ErrInvalidTransition.Wrapf("origin domain changed from %d to %d; an ism is pinned to one origin for life",
			prev.OriginDomain, next.OriginDomain)
	}
	if next.Height <= prev.Height {
		return ErrInvalidTransition.Wrapf("height did not advance: %d to %d", prev.Height, next.Height)
	}
	if next.Timestamp < prev.Timestamp {
		return ErrInvalidTransition.Wrapf("timestamp moved backwards: %d to %d", prev.Timestamp, next.Timestamp)
	}
	if next.StateRoot == prev.StateRoot {
		return ErrInvalidTransition.Wrap("state root unchanged")
	}
	if next.IdentityDigest != prev.IdentityDigest {
		return ErrInvalidTransition.Wrap("enclave identity changed")
	}
	return nil
}

// AttestedUpdate is the single payload an enclave hashes into a quote's
// report_data.
//
// One attestation covers both the state transition and the message batch. That
// is the whole reason this module needs one message where a proof-carrying ISM
// needs two: there was only ever one quote, and the second transaction existed
// only because two proofs had to project the same attestation through two
// different public-value shapes.
type AttestedUpdate struct {
	PrevState *IsmState
	NewState  *IsmState
	// MerkleTreeAddress is the origin merkle tree hook, left-padded to 32 bytes
	// for EVM addresses.
	MerkleTreeAddress [32]byte
	// AttestedAt is the newest chain time the enclave actually verified.
	//
	// Not the same thing as NewState.Timestamp. An optimistic rollup's confirmed
	// head is old by design, because that lag is the fraud proof window, so
	// bounding the submitter's clock against it would reject every honest proof.
	AttestedAt uint64
	// MessageIDs are the messages the enclave proved present in the origin tree
	// at NewState.
	MessageIDs [][32]byte
}

// EncodeAttestedUpdate serializes an update to the form the enclave hashed.
func EncodeAttestedUpdate(u *AttestedUpdate) []byte {
	out := make([]byte, 0, attestedUpdateHead+32*len(u.MessageIDs))
	out = append(out, EncodeIsmState(u.PrevState)...)
	out = append(out, EncodeIsmState(u.NewState)...)
	out = append(out, u.MerkleTreeAddress[:]...)
	out = binary.BigEndian.AppendUint64(out, u.AttestedAt)
	out = binary.BigEndian.AppendUint64(out, uint64(len(u.MessageIDs)))
	for _, id := range u.MessageIDs {
		out = append(out, id[:]...)
	}
	return out
}

// DecodeAttestedUpdate parses an attested update, rejecting both truncation and
// trailing bytes so that exactly one encoding maps to any given update.
func DecodeAttestedUpdate(b []byte) (*AttestedUpdate, error) {
	if len(b) < attestedUpdateHead {
		return nil, fmt.Errorf("attested update needs at least %d bytes, got %d", attestedUpdateHead, len(b))
	}
	prev, err := DecodeIsmState(b[:IsmStateBytes])
	if err != nil {
		return nil, fmt.Errorf("prev state: %w", err)
	}
	next, err := DecodeIsmState(b[IsmStateBytes : 2*IsmStateBytes])
	if err != nil {
		return nil, fmt.Errorf("new state: %w", err)
	}

	u := &AttestedUpdate{PrevState: prev, NewState: next}
	o := 2 * IsmStateBytes
	copy(u.MerkleTreeAddress[:], b[o:o+32])
	o += 32
	u.AttestedAt = binary.BigEndian.Uint64(b[o : o+8])
	o += 8
	count := binary.BigEndian.Uint64(b[o : o+8])
	o += 8

	if count > MaxMessageIDsCount {
		return nil, fmt.Errorf("message id count %d exceeds the maximum of %d", count, MaxMessageIDsCount)
	}
	rest := b[o:]
	// int64 rather than int, so count*32 cannot wrap on a 32-bit platform.
	if int64(len(rest)) != int64(count)*32 {
		return nil, fmt.Errorf("message ids need exactly %d bytes, have %d", int64(count)*32, len(rest))
	}

	u.MessageIDs = make([][32]byte, count)
	for i := range u.MessageIDs {
		copy(u.MessageIDs[i][:], rest[i*32:(i+1)*32])
	}
	return u, nil
}

// HashAttestedUpdate is the value the enclave places in the first 32 bytes of
// the quote's report_data.
func HashAttestedUpdate(u *AttestedUpdate) [32]byte {
	return sha256.Sum256(EncodeAttestedUpdate(u))
}

// StateRootOf returns the first 32 bytes of a raw state blob.
func StateRootOf(state []byte) ([]byte, error) {
	if len(state) < MinStateBytes {
		return nil, ErrInvalidTrustedState.Wrapf("state must be at least %d bytes, got %d", MinStateBytes, len(state))
	}
	return state[:32], nil
}

// EqualState reports whether two raw state blobs are identical.
func EqualState(a, b []byte) bool { return bytes.Equal(a, b) }
