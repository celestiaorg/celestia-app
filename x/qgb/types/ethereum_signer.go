package types

import (
	"crypto/ecdsa"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	signaturePrefix = "\x19Ethereum Signed Message:\n32"
)

// NewEthereumSignature creates a new signature over a given byte array.
func NewEthereumSignature(hash []byte, privateKey *ecdsa.PrivateKey) ([]byte, error) {
	if privateKey == nil {
		return nil, sdkerrors.Wrap(ErrEmpty, "private key")
	}
	protectedHash := crypto.Keccak256Hash([]uint8(signaturePrefix), hash)
	return crypto.Sign(protectedHash.Bytes(), privateKey)
}

func EthAddressFromSignature(hash []byte, signature []byte) (common.Address, error) {
	if len(signature) < 65 {
		return common.Address{}, sdkerrors.Wrap(ErrInvalid, "signature too short")
	}
	protectedHash := crypto.Keccak256Hash([]uint8(signaturePrefix), hash)
	sigPublicKey, err := crypto.Ecrecover(protectedHash.Bytes(), signature)
	if err != nil {
		return common.Address{}, sdkerrors.Wrap(err, "ec recover failed")
	}
	pubKey, err := crypto.UnmarshalPubkey(sigPublicKey)
	if err != nil {
		return common.Address{}, sdkerrors.Wrap(err, "unmarshalling signature public key failed")
	}
	addr := crypto.PubkeyToAddress(*pubKey)
	return addr, nil
}

// ValidateEthereumSignature takes a message, an associated signature and public key and
// returns an error if the signature isn't valid.
func ValidateEthereumSignature(hash []byte, signature []byte, ethAddress common.Address) error {
	addr, err := EthAddressFromSignature(hash, signature)
	if err != nil {
		return sdkerrors.Wrap(err, "unable to get address from signature")
	}

	if addr.Hex() != ethAddress.Hex() {
		return sdkerrors.Wrap(ErrInvalid, "signature not matching")
	}

	return nil
}
