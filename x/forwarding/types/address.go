package types

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/cosmos/cosmos-sdk/types/address"
)

const (
	ForwardVersion  = uint8(1)
	RecipientLength = 32
)

// DeriveForwardingAddress computes a deterministic forwarding address from destination parameters.
// One address handles all tokens for a given (destDomain, destRecipient) pair.
//
// Algorithm:
//  1. callDigest = sha256(destDomain_32bytes || destRecipient)
//  2. salt = sha256(ForwardVersion || callDigest)
//  3. address = address.Module("forwarding", salt)[:20]
//
// Returns an error if destRecipient is not exactly RecipientLength (32) bytes.
func DeriveForwardingAddress(destDomain uint32, destRecipient []byte) ([]byte, error) {
	if len(destRecipient) != RecipientLength {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidRecipient, RecipientLength, len(destRecipient))
	}

	// Step 1: Encode destDomain as 32-byte big-endian (right-aligned)
	destDomainBytes := make([]byte, 32)
	binary.BigEndian.PutUint32(destDomainBytes[28:], destDomain)

	// Step 2: callDigest = sha256(destDomain || destRecipient)
	callDigestPreimage := make([]byte, 32+RecipientLength)
	copy(callDigestPreimage, destDomainBytes)
	copy(callDigestPreimage[32:], destRecipient)
	callDigest := sha256.Sum256(callDigestPreimage)

	// Step 3: salt = sha256(ForwardVersion || callDigest)
	saltPreimage := make([]byte, 1+32)
	saltPreimage[0] = ForwardVersion
	copy(saltPreimage[1:], callDigest[:])
	salt := sha256.Sum256(saltPreimage)

	// Step 4: Use SDK's address.Module for deterministic derivation
	addr := address.Module(ModuleName, salt[:])

	return addr[:20], nil
}
