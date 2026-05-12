package eip712

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	txsigning "cosmossdk.io/x/tx/signing"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
)

const (
	// DomainName is the EIP-712 domain name used for Celestia transactions.
	DomainName = "Celestia"
	// DomainVersion is the EIP-712 domain version used for Celestia transactions.
	DomainVersion = "1"

	domainType = "EIP712Domain(string name,string version,uint256 chainId)"
	txType     = "CelestiaTx(string celestiaChainId,uint256 ethChainId,uint64 accountNumber,uint64 sequence,string signer,string feePayer,string feeGranter,uint64 gasLimit,string feeAmount,bytes32 bodyBytesHash,bytes32 authInfoBytesHash,uint32 schemaVersion)"
)

var (
	domainTypeID = keccak256([]byte(domainType))
	txTypeID     = keccak256([]byte(txType))
)

// TypedData is the JSON shape passed to eth_signTypedData_v4.
type TypedData struct {
	Types       map[string][]TypedDataField `json:"types"`
	PrimaryType string                      `json:"primaryType"`
	Domain      TypedDataDomain             `json:"domain"`
	Message     TypedDataMessage            `json:"message"`
}

// TypedDataField describes one field in an EIP-712 type definition.
type TypedDataField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// TypedDataDomain is the EIP-712 domain for Celestia transaction signing.
type TypedDataDomain struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	ChainID string `json:"chainId"`
}

// TypedDataMessage contains the consensus-bound Celestia transaction fields
// included in the EIP-712 message hash.
type TypedDataMessage struct {
	CelestiaChainID   string `json:"celestiaChainId"`
	EthChainID        string `json:"ethChainId"`
	AccountNumber     string `json:"accountNumber"`
	Sequence          string `json:"sequence"`
	Signer            string `json:"signer"`
	FeePayer          string `json:"feePayer"`
	FeeGranter        string `json:"feeGranter"`
	GasLimit          string `json:"gasLimit"`
	FeeAmount         string `json:"feeAmount"`
	BodyBytesHash     string `json:"bodyBytesHash"`
	AuthInfoBytesHash string `json:"authInfoBytesHash"`
	SchemaVersion     string `json:"schemaVersion"`
}

// BuildTypedData builds the deterministic EIP-712 typed-data payload for a
// Celestia transaction.
func BuildTypedData(signerData txsigning.SignerData, txData txsigning.TxData) (TypedData, error) {
	fields, ext, err := fieldsFromTx(signerData, txData)
	if err != nil {
		return TypedData{}, err
	}

	return TypedData{
		Types: map[string][]TypedDataField{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
			},
			"CelestiaTx": {
				{Name: "celestiaChainId", Type: "string"},
				{Name: "ethChainId", Type: "uint256"},
				{Name: "accountNumber", Type: "uint64"},
				{Name: "sequence", Type: "uint64"},
				{Name: "signer", Type: "string"},
				{Name: "feePayer", Type: "string"},
				{Name: "feeGranter", Type: "string"},
				{Name: "gasLimit", Type: "uint64"},
				{Name: "feeAmount", Type: "string"},
				{Name: "bodyBytesHash", Type: "bytes32"},
				{Name: "authInfoBytesHash", Type: "bytes32"},
				{Name: "schemaVersion", Type: "uint32"},
			},
		},
		PrimaryType: "CelestiaTx",
		Domain: TypedDataDomain{
			Name:    DomainName,
			Version: DomainVersion,
			ChainID: fmt.Sprintf("%d", ext.EthChainId),
		},
		Message: TypedDataMessage{
			CelestiaChainID:   fields.CelestiaChainID,
			EthChainID:        fmt.Sprintf("%d", fields.EthChainID),
			AccountNumber:     fmt.Sprintf("%d", fields.AccountNumber),
			Sequence:          fmt.Sprintf("%d", fields.Sequence),
			Signer:            fields.Signer,
			FeePayer:          fields.FeePayer,
			FeeGranter:        fields.FeeGranter,
			GasLimit:          fmt.Sprintf("%d", fields.GasLimit),
			FeeAmount:         fields.FeeAmount,
			BodyBytesHash:     "0x" + hex.EncodeToString(fields.BodyBytesHash[:]),
			AuthInfoBytesHash: "0x" + hex.EncodeToString(fields.AuthInfoBytesHash[:]),
			SchemaVersion:     fmt.Sprintf("%d", fields.SchemaVersion),
		},
	}, nil
}

// Digest computes the EIP-712 digest signed by Ethereum wallets for the
// provided Celestia transaction data.
func Digest(signerData txsigning.SignerData, txData txsigning.TxData) ([32]byte, error) {
	fields, _, err := fieldsFromTx(signerData, txData)
	if err != nil {
		return [32]byte{}, err
	}

	domainSeparator := keccak256(
		domainTypeID,
		keccak256([]byte(DomainName)),
		keccak256([]byte(DomainVersion)),
		uint256(fields.EthChainID),
	)
	messageHash := keccak256(
		txTypeID,
		keccak256([]byte(fields.CelestiaChainID)),
		uint256(fields.EthChainID),
		uint256(fields.AccountNumber),
		uint256(fields.Sequence),
		keccak256([]byte(fields.Signer)),
		keccak256([]byte(fields.FeePayer)),
		keccak256([]byte(fields.FeeGranter)),
		uint256(fields.GasLimit),
		keccak256([]byte(fields.FeeAmount)),
		fields.BodyBytesHash[:],
		fields.AuthInfoBytesHash[:],
		uint256(uint64(fields.SchemaVersion)),
	)
	return toArray32(keccak256([]byte{0x19, 0x01}, domainSeparator, messageHash)), nil
}

// txFields contains the normalized transaction fields used to construct the
// EIP-712 typed-data payload and digest.
type txFields struct {
	CelestiaChainID   string
	EthChainID        uint64
	AccountNumber     uint64
	Sequence          uint64
	Signer            string
	FeePayer          string
	FeeGranter        string
	GasLimit          uint64
	FeeAmount         string
	BodyBytesHash     [32]byte
	AuthInfoBytesHash [32]byte
	SchemaVersion     uint32
}

// fieldsFromTx extracts the consensus-bound EIP-712 fields from signer and
// transaction data.
func fieldsFromTx(signerData txsigning.SignerData, txData txsigning.TxData) (txFields, *ExtensionOptionsEIP712, error) {
	if txData.Body == nil {
		return txFields{}, nil, fmt.Errorf("missing tx body")
	}

	if txData.AuthInfo == nil || txData.AuthInfo.Fee == nil {
		return txFields{}, nil, fmt.Errorf("missing tx auth info fee")
	}

	ext, err := getExtension(txData)
	if err != nil {
		return txFields{}, nil, err
	}

	bodyHash := sha256.Sum256(txData.BodyBytes)
	authInfoHash := sha256.Sum256(txData.AuthInfoBytes)
	return txFields{
		CelestiaChainID:   signerData.ChainID,
		EthChainID:        ext.EthChainId,
		AccountNumber:     signerData.AccountNumber,
		Sequence:          signerData.Sequence,
		Signer:            signerData.Address,
		FeePayer:          txData.AuthInfo.Fee.Payer,
		FeeGranter:        txData.AuthInfo.Fee.Granter,
		GasLimit:          txData.AuthInfo.Fee.GasLimit,
		FeeAmount:         feeAmountString(txData),
		BodyBytesHash:     bodyHash,
		AuthInfoBytesHash: authInfoHash,
		SchemaVersion:     ext.SchemaVersion,
	}, ext, nil
}

// getExtension returns the single EIP-712 critical extension option from a
// transaction.
func getExtension(txData txsigning.TxData) (*ExtensionOptionsEIP712, error) {
	var found *ExtensionOptionsEIP712
	for _, opt := range txData.Body.ExtensionOptions {
		if opt.TypeUrl != ExtensionOptionsTypeURL {
			continue
		}

		legacyAny := &codectypes.Any{TypeUrl: opt.TypeUrl, Value: opt.Value}
		ext, err := DecodeExtensionOption(legacyAny)
		if err != nil {
			return nil, err
		}

		if found != nil {
			return nil, fmt.Errorf("multiple EIP-712 extension options")
		}
		found = ext
	}

	if found == nil {
		return nil, fmt.Errorf("missing EIP-712 extension option")
	}

	return found, nil
}

// feeAmountString returns a deterministic string encoding of transaction fees.
func feeAmountString(txData txsigning.TxData) string {
	amounts := txData.AuthInfo.Fee.Amount
	parts := make([]string, len(amounts))
	for i, coin := range amounts {
		parts[i] = coin.Amount + coin.Denom
	}
	return strings.Join(parts, ",")
}

// keccak256 returns the Keccak-256 hash of the concatenated input chunks.
func keccak256(data ...[]byte) []byte {
	return gethcrypto.Keccak256(data...)
}

// uint256 returns the ABI-style 32-byte big-endian encoding of v.
func uint256(v uint64) []byte {
	var out [32]byte
	new(big.Int).SetUint64(v).FillBytes(out[:])
	return out[:]
}

// toArray32 copies b into a fixed 32-byte array.
func toArray32(b []byte) [32]byte {
	var out [32]byte
	copy(out[:], b)
	return out
}
