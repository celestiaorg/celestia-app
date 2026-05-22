package ethereum

import (
	"fmt"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/gogoproto/proto"
)

const (
	// ExtensionOptionsTypeURL is the protobuf type URL for the Celestia
	// Ethereum transaction critical extension option.
	ExtensionOptionsTypeURL = "/celestia.tx.v1.ExtensionOptionsEthereumTx"

	// SchemaVersion is the supported Ethereum transaction authorization schema
	// version.
	SchemaVersion = uint32(1)
)

// RegisterInterfaces registers the Ethereum transaction extension option as a
// Cosmos SDK transaction extension option implementation.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*txtypes.TxExtensionOptionI)(nil), &ExtensionOptionsEthereumTx{})
}

// NewExtensionOptions returns an Any containing the Ethereum transaction
// critical extension option.
func NewExtensionOptions(schemaVersion uint32, ethChainID uint64, rawTransaction []byte) (*codectypes.Any, error) {
	return codectypes.NewAnyWithValue(&ExtensionOptionsEthereumTx{
		SchemaVersion:  schemaVersion,
		EthChainId:     ethChainID,
		RawTransaction: rawTransaction,
	})
}

// ExtensionOptionChecker reports whether opt is a supported Ethereum
// transaction extension option.
func ExtensionOptionChecker(opt *codectypes.Any) bool {
	_, err := DecodeExtensionOption(opt)
	return err == nil
}

// DecodeExtensionOption decodes and validates an Ethereum transaction critical
// extension option.
func DecodeExtensionOption(opt *codectypes.Any) (*ExtensionOptionsEthereumTx, error) {
	if opt == nil {
		return nil, fmt.Errorf("missing Ethereum transaction extension option")
	}

	if opt.TypeUrl != ExtensionOptionsTypeURL {
		return nil, fmt.Errorf("unsupported extension option type %s", opt.TypeUrl)
	}

	var ext ExtensionOptionsEthereumTx
	if err := proto.Unmarshal(opt.Value, &ext); err != nil {
		return nil, err
	}

	if ext.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported Ethereum transaction schema version %d", ext.SchemaVersion)
	}

	if ext.EthChainId == 0 {
		return nil, fmt.Errorf("Ethereum transaction chain ID must be non-zero")
	}

	if len(ext.RawTransaction) == 0 {
		return nil, fmt.Errorf("Ethereum transaction raw transaction must be non-empty")
	}

	return &ext, nil
}
