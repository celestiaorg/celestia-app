package types

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/cosmos/cosmos-sdk/types/address"
)

const (
	// ForwardVersion is the version of the forwarding address derivation algorithm.
	// Incrementing this allows address scheme upgrades without collision.
	ForwardVersion  = uint8(1)
	RecipientLength = 32
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
// One address handles all tokens for a given (destDomain, destRecipient) pair.
//
// Algorithm:
//  1. callDigest = sha256(destDomain_32bytes || destRecipient)
//  2. salt = sha256(ForwardVersion || callDigest)
//  3. address = address.Module("forwarding", salt)[:CosmosAddressLen]
//
// Returns an error if destRecipient is not exactly RecipientLength (32) bytes.
func DeriveForwardingAddress(destDomain uint32, destRecipient []byte) ([]byte, error) {
	if len(destRecipient) != RecipientLength {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidRecipient, RecipientLength, len(destRecipient))
	}

	// Step 1: Encode destDomain as 32-byte big-endian (right-aligned, ABI uint256 encoding)
	destDomainBytes := make([]byte, DomainEncodingSize)
	binary.BigEndian.PutUint32(destDomainBytes[DomainOffset:], destDomain)

	// Step 2: callDigest = sha256(destDomain || destRecipient)
	h := sha256.New()
	h.Write(destDomainBytes)
	h.Write(destRecipient)
	callDigest := h.Sum(nil)

	// Step 3: salt = sha256(ForwardVersion || callDigest)
	h.Reset()
	h.Write([]byte{ForwardVersion})
	h.Write(callDigest)
	salt := h.Sum(nil)

	// Step 4: Use SDK's address.Module for deterministic derivation
	addr := address.Module(ModuleName, salt)

	return addr[:CosmosAddressLen], nil
}
