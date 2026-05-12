package eip712

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	signingv1beta1 "cosmossdk.io/api/cosmos/tx/signing/v1beta1"
	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
)

// SignModeHandler implements SIGN_MODE_EIP_712 for Celestia native
// transactions.
type SignModeHandler struct{}

// Mode returns the Cosmos SDK sign mode handled by SignModeHandler.
func (SignModeHandler) Mode() signingv1beta1.SignMode {
	return signingv1beta1.SignMode_SIGN_MODE_EIP_712
}

// GetSignBytes returns the JSON EIP-712 typed-data payload that an Ethereum
// wallet signs for a Celestia transaction.
func (SignModeHandler) GetSignBytes(_ context.Context, signerData txsigning.SignerData, txData txsigning.TxData) ([]byte, error) {
	payload, err := BuildTypedData(signerData, txData)
	if err != nil {
		return nil, err
	}

	return json.Marshal(payload)
}

// VerifySignature verifies an EIP-712 signature by recovering the secp256k1
// public key from the typed-data digest and comparing it with the signer data.
func (SignModeHandler) VerifySignature(_ context.Context, pubKey cryptotypes.PubKey, signerData txsigning.SignerData, signature []byte, txData txsigning.TxData) error {
	recovered, err := RecoverPubKey(signerData, txData, signature)
	if err != nil {
		return err
	}

	if pubKey != nil && !bytes.Equal(pubKey.Bytes(), recovered.Bytes()) {
		return fmt.Errorf("EIP-712 recovered pubkey does not match signer pubkey")
	}

	if signerData.Address != "" {
		recoveredAddr := sdk.AccAddress(recovered.Address()).String()
		if recoveredAddr != signerData.Address {
			return fmt.Errorf("EIP-712 recovered signer %s does not match canonical signer %s", recoveredAddr, signerData.Address)
		}
	}

	return nil
}

// RecoverPubKey recovers the compressed Cosmos SDK secp256k1 public key from
// an EIP-712 signature over the Celestia transaction digest.
func RecoverPubKey(signerData txsigning.SignerData, txData txsigning.TxData, signature []byte) (*secp256k1.PubKey, error) {
	digest, err := Digest(signerData, txData)
	if err != nil {
		return nil, err
	}

	ethSig, err := normalizeSignature(signature)
	if err != nil {
		return nil, err
	}

	recovered, err := gethcrypto.SigToPub(digest[:], ethSig)
	if err != nil {
		return nil, err
	}

	return &secp256k1.PubKey{Key: gethcrypto.CompressPubkey(recovered)}, nil
}

// EthereumAddress derives the Ethereum address for a compressed Cosmos SDK
// secp256k1 public key.
func EthereumAddress(pubKey *secp256k1.PubKey) ([20]byte, error) {
	parsed, err := gethcrypto.DecompressPubkey(pubKey.Key)
	if err != nil {
		return [20]byte{}, err
	}

	gethAddr := gethcrypto.PubkeyToAddress(*parsed)
	var out [20]byte
	copy(out[:], gethAddr.Bytes())
	return out, nil
}

// IsEIP712SignatureData reports whether data is a single EIP-712 signature.
func IsEIP712SignatureData(data signingtypes.SignatureData) bool {
	single, ok := data.(*signingtypes.SingleSignatureData)
	return ok && single.SignMode == signingtypes.SignMode_SIGN_MODE_EIP_712
}

// normalizeSignature validates a 65-byte Ethereum signature and normalizes the
// recovery ID to the 0/1 form required by go-ethereum recovery helpers.
func normalizeSignature(sig []byte) ([]byte, error) {
	if len(sig) != 65 {
		return nil, fmt.Errorf("EIP-712 signature must be 65 bytes, got %d", len(sig))
	}

	v := sig[64]
	switch v {
	case 27, 28:
		v -= 27
	case 0, 1:
	default:
		return nil, fmt.Errorf("EIP-712 signature has invalid V value %d", sig[64])
	}

	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])
	if !gethcrypto.ValidateSignatureValues(v, r, s, true) {
		return nil, fmt.Errorf("EIP-712 signature has invalid values")
	}

	normalized := append([]byte{}, sig...)
	normalized[64] = v
	return normalized, nil
}
