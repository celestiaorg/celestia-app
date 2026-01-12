package types

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	// ForwardVersionPrefix is the version prefix used in salt derivation
	ForwardVersionPrefix = "CELESTIA_FORWARD_V1"

	// RecipientLength is the required length for cross-chain recipient addresses
	RecipientLength = 32
)

// Recipient32 is a validated 32-byte recipient address for cross-chain transfers.
// Using this type instead of []byte provides compile-time guarantees about length.
type Recipient32 [RecipientLength]byte

// NewRecipient32 creates a Recipient32 from a byte slice, validating the length.
func NewRecipient32(b []byte) (Recipient32, error) {
	if len(b) != RecipientLength {
		return Recipient32{}, fmt.Errorf("recipient must be %d bytes, got %d", RecipientLength, len(b))
	}
	var r Recipient32
	copy(r[:], b)
	return r, nil
}

// Bytes returns the underlying byte slice.
func (r Recipient32) Bytes() []byte {
	return r[:]
}

// DeriveForwardingAddress computes a deterministic forwarding address from destination parameters.
// One address handles all tokens for a given (destDomain, destRecipient) pair.
//
// Algorithm:
// 1. callDigest = keccak256(abi.encode(destDomain, destRecipient))
// 2. salt = keccak256("CELESTIA_FORWARD_V1" || callDigest)
// 3. address = sha256(moduleName || salt)[:20]
func DeriveForwardingAddress(destDomain uint32, destRecipient []byte) sdk.AccAddress {
	// Step 1: Encode destDomain as 32-byte big-endian (right-aligned at bytes[28:32])
	destDomainBytes := make([]byte, 32)
	binary.BigEndian.PutUint32(destDomainBytes[28:], destDomain)

	// Step 2: Compute call digest
	// callDigest = keccak256(destDomainBytes || destRecipient)
	callDigestPreimage := append(destDomainBytes, destRecipient...)
	callDigest := crypto.Keccak256(callDigestPreimage)

	// Step 3: Compute salt with version prefix
	// salt = keccak256("CELESTIA_FORWARD_V1" || callDigest)
	saltPreimage := append([]byte(ForwardVersionPrefix), callDigest...)
	salt := crypto.Keccak256(saltPreimage)

	// Step 4: Derive address
	// address = sha256(moduleName || salt)[:20]
	addressPreimage := append([]byte(ModuleName), salt...)
	hash := sha256.Sum256(addressPreimage)

	return sdk.AccAddress(hash[:20])
}
