package types

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/cosmos/cosmos-sdk/types/address"
)

const (
	// ForwardVersion is the version of the forwarding address derivation algorithm
	// for addresses that use the mailbox default post-dispatch hook.
	// Incrementing this allows address scheme upgrades without collision.
	ForwardVersion = uint8(1)
	// ForwardVersionHook is the derivation version for addresses that commit to a
	// specific post-dispatch hook. Addresses derived under this version can only be
	// forwarded through the hook they were derived with.
	ForwardVersionHook = uint8(2)
	// RecipientLength is 32 bytes - the Hyperlane standard for cross-chain recipient addresses.
	// EVM 20-byte addresses must be left-padded with 12 zero bytes to meet this requirement.
	RecipientLength = 32
	// TokenIDLength is 32 bytes - the Hyperlane token identifier length.
	TokenIDLength = 32
	// HookIDLength is 32 bytes - the Hyperlane post-dispatch hook identifier length.
	HookIDLength = 32
	// DomainEncodingSize is the byte size for ABI-encoding domain IDs (uint256).
	DomainEncodingSize = 32
	// DomainOffset is where uint32 is placed in the 32-byte buffer (right-aligned).
	DomainOffset = DomainEncodingSize - 4
	// HashSize is the output size of SHA-256.
	HashSize = 32
	// CosmosAddressLen is the standard Cosmos SDK address length (20 bytes).
	CosmosAddressLen = 20
)

// DeriveForwardingAddress computes a deterministic forwarding address from destination parameters.
// Each address is bound to a single token for a given (destDomain, destRecipient, tokenID) tuple.
// Addresses derived here use the mailbox default post-dispatch hook; a forward that supplies a
// custom hook derives a different address (see DeriveForwardingAddressWithHook) and will not match.
//
// Algorithm:
//  1. callDigest = sha256(destDomain_32bytes || destRecipient || tokenID)
//  2. salt = sha256(ForwardVersion || callDigest)
//  3. address = address.Module("forwarding", salt)[:CosmosAddressLen]
//
// Returns ErrInvalidRecipient if destRecipient is not exactly RecipientLength (32) bytes,
// and ErrInvalidTokenID if tokenID is not exactly TokenIDLength (32) bytes.
func DeriveForwardingAddress(destDomain uint32, destRecipient, tokenID []byte) ([]byte, error) {
	return deriveForwardingAddress(ForwardVersion, destDomain, destRecipient, tokenID, nil)
}

// DeriveForwardingAddressWithHook computes a forwarding address that additionally commits to a
// specific post-dispatch hook. This makes the hook part of the depositor's intent rather than a
// parameter the submitter of MsgForward chooses: forwarding through any other hook derives a
// different address and is rejected with ErrAddressMismatch.
//
// Algorithm (identical to DeriveForwardingAddress except for the hook and the version byte):
//  1. callDigest = sha256(destDomain_32bytes || destRecipient || tokenID || hookID)
//  2. salt = sha256(ForwardVersionHook || callDigest)
//  3. address = address.Module("forwarding", salt)[:CosmosAddressLen]
//
// hookID must be exactly HookIDLength (32) bytes and must not be the zero address; the zero
// address means "mailbox default hook", which is what DeriveForwardingAddress already covers.
func DeriveForwardingAddressWithHook(destDomain uint32, destRecipient, tokenID, hookID []byte) ([]byte, error) {
	if len(hookID) != HookIDLength {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidHookID, HookIDLength, len(hookID))
	}

	if isZeroBytes(hookID) {
		return nil, fmt.Errorf("%w: zero hook id means the mailbox default hook, use DeriveForwardingAddress", ErrInvalidHookID)
	}

	return deriveForwardingAddress(ForwardVersionHook, destDomain, destRecipient, tokenID, hookID)
}

// deriveForwardingAddress is the shared derivation. hookID is appended to the digest only when
// non-nil, so the version byte and the digest preimage move together and the two schemes cannot
// collide.
func deriveForwardingAddress(version uint8, destDomain uint32, destRecipient, tokenID, hookID []byte) ([]byte, error) {
	if len(destRecipient) != RecipientLength {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidRecipient, RecipientLength, len(destRecipient))
	}

	if len(tokenID) != TokenIDLength {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidTokenID, TokenIDLength, len(tokenID))
	}

	// Step 1: Encode destDomain as 32-byte big-endian (right-aligned, ABI uint256 encoding)
	destDomainBytes := make([]byte, DomainEncodingSize)
	binary.BigEndian.PutUint32(destDomainBytes[DomainOffset:], destDomain)

	// Step 2: callDigest = sha256(destDomain || destRecipient || tokenID [|| hookID])
	h := sha256.New()
	h.Write(destDomainBytes)
	h.Write(destRecipient)
	h.Write(tokenID)
	if hookID != nil {
		h.Write(hookID)
	}
	callDigest := h.Sum(nil)

	// Step 3: salt = sha256(version || callDigest)
	h.Reset()
	h.Write([]byte{version})
	h.Write(callDigest)
	salt := h.Sum(nil)

	// Step 4: Use SDK's address.Module for deterministic derivation
	addr := address.Module(ModuleName, salt)

	return addr[:CosmosAddressLen], nil
}

func isZeroBytes(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
