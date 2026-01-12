package types

import (
	"crypto/sha256"
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	ForwardVersionPrefix = "CELESTIA_FORWARD_V1"
	RecipientLength      = 32
)

// DeriveForwardingAddress computes a deterministic forwarding address from destination parameters.
// One address handles all tokens for a given (destDomain, destRecipient) pair.
//
// Algorithm:
//  1. callDigest = sha256(destDomain_32bytes || destRecipient)
//  2. salt = sha256("CELESTIA_FORWARD_V1" || callDigest)
//  3. address = sha256(moduleName || salt)[:20]
//
// Panics if destRecipient is not exactly RecipientLength (32) bytes.
func DeriveForwardingAddress(destDomain uint32, destRecipient []byte) sdk.AccAddress {
	if len(destRecipient) != RecipientLength {
		panic("destRecipient must be exactly 32 bytes")
	}

	// Step 1: Encode destDomain as 32-byte big-endian (right-aligned)
	destDomainBytes := make([]byte, 32)
	binary.BigEndian.PutUint32(destDomainBytes[28:], destDomain)

	// Step 2: callDigest = sha256(destDomain || destRecipient)
	callDigestPreimage := append(destDomainBytes, destRecipient...)
	callDigest := sha256.Sum256(callDigestPreimage)

	// Step 3: salt = sha256("CELESTIA_FORWARD_V1" || callDigest)
	saltPreimage := append([]byte(ForwardVersionPrefix), callDigest[:]...)
	salt := sha256.Sum256(saltPreimage)

	// Step 4: address = sha256(moduleName || salt)[:20]
	addressPreimage := append([]byte(ModuleName), salt[:]...)
	hash := sha256.Sum256(addressPreimage)

	return sdk.AccAddress(hash[:20])
}
