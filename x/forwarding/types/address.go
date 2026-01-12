package types

import (
	"crypto/sha256"
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	ForwardVersionPrefix = "CELESTIA_FORWARD_V1"
	RecipientLength      = 32
)

// DeriveForwardingAddress computes a deterministic forwarding address from destination parameters.
// One address handles all tokens for a given (destDomain, destRecipient) pair.
//
// Algorithm:
//  1. callDigest = keccak256(abi.encode(destDomain, destRecipient))
//  2. salt = keccak256("CELESTIA_FORWARD_V1" || callDigest)
//  3. address = sha256(moduleName || salt)[:20]
//
// Panics if destRecipient is not exactly RecipientLength (32) bytes.
func DeriveForwardingAddress(destDomain uint32, destRecipient []byte) sdk.AccAddress {
	if len(destRecipient) != RecipientLength {
		panic("destRecipient must be exactly 32 bytes")
	}

	destDomainBytes := make([]byte, 32)
	binary.BigEndian.PutUint32(destDomainBytes[28:], destDomain)

	callDigest := crypto.Keccak256(append(destDomainBytes, destRecipient...))
	salt := crypto.Keccak256(append([]byte(ForwardVersionPrefix), callDigest...))

	addressPreimage := append([]byte(ModuleName), salt...)
	hash := sha256.Sum256(addressPreimage)

	return sdk.AccAddress(hash[:20])
}
