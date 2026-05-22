package ethereum

import (
	"bytes"
	"context"
	"fmt"
	"math/big"

	signingv1beta1 "cosmossdk.io/api/cosmos/tx/signing/v1beta1"
	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	gethcommon "github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
)

// SignModeHandler implements SIGN_MODE_ETHEREUM_TX for translated Ethereum
// transaction envelopes.
type SignModeHandler struct{}

const (
	// SignModeAPI is the protobuf sign mode value registered in the Celestia
	// Cosmos SDK fork for Ethereum transaction envelope signatures.
	SignModeAPI signingv1beta1.SignMode = 1559

	// SignMode is the legacy SDK sign mode value registered in the Celestia
	// Cosmos SDK fork for Ethereum transaction envelope signatures.
	SignMode signingtypes.SignMode = 1559
)

// Mode returns the Cosmos SDK sign mode handled by SignModeHandler.
func (SignModeHandler) Mode() signingv1beta1.SignMode {
	return SignModeAPI
}

// GetSignBytes returns the Ethereum transaction signing hash for the preserved
// raw transaction envelope.
func (SignModeHandler) GetSignBytes(_ context.Context, signerData txsigning.SignerData, txData txsigning.TxData) ([]byte, error) {
	auth, err := AuthorizationFromTxData(signerData, txData)
	if err != nil {
		return nil, err
	}
	return auth.SignHash.Bytes(), nil
}

// VerifySignature verifies the SIGN_MODE_ETHEREUM_TX signature by recovering
// the secp256k1 public key from the preserved Ethereum transaction envelope.
func (SignModeHandler) VerifySignature(_ context.Context, pubKey cryptotypes.PubKey, signerData txsigning.SignerData, signature []byte, txData txsigning.TxData) error {
	auth, err := AuthorizationFromTxData(signerData, txData)
	if err != nil {
		return err
	}

	if !bytes.Equal(signature, auth.Signature) {
		return fmt.Errorf("Ethereum transaction signature does not match preserved envelope signature")
	}

	recovered, err := RecoverPubKey(auth, signature)
	if err != nil {
		return err
	}

	if pubKey != nil && !bytes.Equal(pubKey.Bytes(), recovered.Bytes()) {
		return fmt.Errorf("Ethereum transaction recovered pubkey does not match signer pubkey")
	}

	if signerData.Address != "" {
		recoveredAddr := sdk.AccAddress(recovered.Address()).String()
		if recoveredAddr != signerData.Address {
			return fmt.Errorf("Ethereum transaction recovered signer %s does not match canonical signer %s", recoveredAddr, signerData.Address)
		}
	}

	return nil
}

// Authorization contains the decoded Ethereum transaction authorization data
// preserved in a critical extension option.
type Authorization struct {
	Extension *ExtensionOptionsEthereumTx
	Tx        *gethtypes.Transaction
	Signer    gethtypes.Signer
	SignHash  gethcommon.Hash
	Signature []byte
	From      gethcommon.Address
	To        gethcommon.Address
}

// AuthorizationFromTxData decodes the single Ethereum transaction extension
// option and extracts the signed envelope authorization fields.
func AuthorizationFromTxData(signerData txsigning.SignerData, txData txsigning.TxData) (Authorization, error) {
	ext, err := getExtension(txData)
	if err != nil {
		return Authorization{}, err
	}

	var tx gethtypes.Transaction
	if err := tx.UnmarshalBinary(ext.RawTransaction); err != nil {
		return Authorization{}, fmt.Errorf("decode Ethereum transaction envelope: %w", err)
	}

	if tx.Type() != gethtypes.DynamicFeeTxType {
		return Authorization{}, fmt.Errorf("unsupported Ethereum transaction type %d", tx.Type())
	}
	if tx.To() == nil {
		return Authorization{}, fmt.Errorf("contract creation is not supported")
	}
	if len(tx.Data()) != 0 {
		return Authorization{}, fmt.Errorf("non-empty data is not supported for native value transfer")
	}
	if len(tx.AccessList()) != 0 {
		return Authorization{}, fmt.Errorf("non-empty access list is not supported for native value transfer")
	}
	if tx.ChainId().Cmp(new(big.Int).SetUint64(ext.EthChainId)) != 0 {
		return Authorization{}, fmt.Errorf("Ethereum transaction chain ID %s does not match extension chain ID %d", tx.ChainId(), ext.EthChainId)
	}

	expectedChainID, err := ChainIDForCelestia(signerData.ChainID)
	if err != nil {
		return Authorization{}, err
	}
	if ext.EthChainId != expectedChainID {
		return Authorization{}, fmt.Errorf("Ethereum transaction extension chain ID %d does not match Celestia chain ID %s", ext.EthChainId, signerData.ChainID)
	}

	signer := gethtypes.LatestSignerForChainID(tx.ChainId())
	signature, err := SignatureFromTx(&tx)
	if err != nil {
		return Authorization{}, err
	}
	from, err := gethtypes.Sender(signer, &tx)
	if err != nil {
		return Authorization{}, fmt.Errorf("recover Ethereum transaction sender: %w", err)
	}

	return Authorization{
		Extension: ext,
		Tx:        &tx,
		Signer:    signer,
		SignHash:  signer.Hash(&tx),
		Signature: signature,
		From:      from,
		To:        *tx.To(),
	}, nil
}

// RecoverPubKey recovers the compressed Cosmos SDK secp256k1 public key from
// an Ethereum transaction signature.
func RecoverPubKey(auth Authorization, signature []byte) (*secp256k1.PubKey, error) {
	recovered, err := gethcrypto.SigToPub(auth.SignHash.Bytes(), signature)
	if err != nil {
		return nil, err
	}
	return &secp256k1.PubKey{Key: gethcrypto.CompressPubkey(recovered)}, nil
}

// EthereumAddress derives the Ethereum address for a compressed Cosmos SDK
// secp256k1 public key.
func EthereumAddress(pubKey *secp256k1.PubKey) (gethcommon.Address, error) {
	parsed, err := gethcrypto.DecompressPubkey(pubKey.Key)
	if err != nil {
		return gethcommon.Address{}, err
	}
	return gethcrypto.PubkeyToAddress(*parsed), nil
}

// IsEthereumTxSignatureData reports whether data is a single Ethereum
// transaction signature.
func IsEthereumTxSignatureData(data signingtypes.SignatureData) bool {
	single, ok := data.(*signingtypes.SingleSignatureData)
	return ok && single.SignMode == SignMode
}

func getExtension(txData txsigning.TxData) (*ExtensionOptionsEthereumTx, error) {
	if txData.Body == nil {
		return nil, fmt.Errorf("missing tx body")
	}

	var found *ExtensionOptionsEthereumTx
	for _, opt := range txData.Body.ExtensionOptions {
		if opt.TypeUrl != ExtensionOptionsTypeURL {
			continue
		}

		legacyAny := &types.Any{TypeUrl: opt.TypeUrl, Value: opt.Value}
		ext, err := DecodeExtensionOption(legacyAny)
		if err != nil {
			return nil, err
		}

		if found != nil {
			return nil, fmt.Errorf("multiple Ethereum transaction extension options")
		}
		found = ext
	}

	if found == nil {
		return nil, fmt.Errorf("missing Ethereum transaction extension option")
	}

	return found, nil
}

// SignatureFromTx returns the compact r || s || yParity signature bytes from a
// supported Ethereum transaction.
func SignatureFromTx(tx *gethtypes.Transaction) ([]byte, error) {
	v, r, s := tx.RawSignatureValues()
	if !v.IsUint64() {
		return nil, fmt.Errorf("Ethereum transaction signature V value overflows uint64")
	}
	vByte := byte(v.Uint64())
	if vByte != 0 && vByte != 1 {
		return nil, fmt.Errorf("Ethereum transaction signature has invalid y-parity %d", vByte)
	}
	if !gethcrypto.ValidateSignatureValues(vByte, r, s, true) {
		return nil, fmt.Errorf("Ethereum transaction signature has invalid values")
	}

	signature := make([]byte, 65)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:64])
	signature[64] = vByte
	return signature, nil
}
