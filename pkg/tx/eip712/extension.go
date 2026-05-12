package eip712

import (
	"fmt"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/gogoproto/proto"
)

const (
	// ExtensionOptionsTypeURL is the protobuf type URL for the Celestia
	// EIP-712 critical extension option.
	ExtensionOptionsTypeURL = "/celestia.tx.v1.ExtensionOptionsEIP712"

	// SchemaVersion is the supported Celestia EIP-712 typed-data schema version.
	SchemaVersion = uint32(1)
)

// RegisterInterfaces registers the EIP-712 extension option as a Cosmos SDK
// transaction extension option implementation.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*txtypes.TxExtensionOptionI)(nil), &ExtensionOptionsEIP712{})
}

// NewExtensionOptions returns an Any containing the EIP-712 critical extension
// option for the provided schema version and Ethereum domain chain ID.
func NewExtensionOptions(schemaVersion uint32, ethChainID uint64) (*codectypes.Any, error) {
	return codectypes.NewAnyWithValue(&ExtensionOptionsEIP712{
		SchemaVersion: schemaVersion,
		EthChainId:    ethChainID,
	})
}

// ExtensionOptionChecker reports whether opt is a supported EIP-712 extension
// option.
func ExtensionOptionChecker(opt *codectypes.Any) bool {
	_, err := DecodeExtensionOption(opt)
	return err == nil
}

// DecodeExtensionOption decodes and validates an EIP-712 critical extension
// option.
func DecodeExtensionOption(opt *codectypes.Any) (*ExtensionOptionsEIP712, error) {
	if opt == nil {
		return nil, fmt.Errorf("missing EIP-712 extension option")
	}

	if opt.TypeUrl != ExtensionOptionsTypeURL {
		return nil, fmt.Errorf("unsupported extension option type %s", opt.TypeUrl)
	}

	var ext ExtensionOptionsEIP712
	if err := proto.Unmarshal(opt.Value, &ext); err != nil {
		return nil, err
	}

	if ext.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported EIP-712 schema version %d", ext.SchemaVersion)
	}

	if ext.EthChainId == 0 {
		return nil, fmt.Errorf("EIP-712 Ethereum chain ID must be non-zero")
	}

	return &ext, nil
}
