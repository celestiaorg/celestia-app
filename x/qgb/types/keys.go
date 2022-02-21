package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"strings"
)

const (
	// ModuleName is the name of the module
	ModuleName = "gqb"

	// StoreKey to be used when creating the KVStore
	StoreKey = ModuleName

	// RouterKey is the module name router key
	RouterKey = ModuleName

	// QuerierRoute to be used for querierer msgs
	QuerierRoute = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_payment"
)

var (
	// ValsetConfirmKey indexes valset confirmations by nonce and the validator account address
	// FIXME: For our keys, should they be `gravity1ahx7f8wyertuus9r20284ej0asrs085ceqtfnm` or `qgb1ahx7f8wyertuus9r20284ej0asrs085ceqtfnm`?
	// i.e gravity1ahx7f8wyertuus9r20284ej0asrs085ceqtfnm
	ValsetConfirmKey = "ValsetConfirmKey"
	// EthAddressByValidatorKey indexes cosmos validator account addresses
	// i.e. gravity1ahx7f8wyertuus9r20284ej0asrs085ceqtfnm
	EthAddressByValidatorKey = "EthAddressValidatorKey"
	// KeyOrchestratorAddress indexes the validator keys for an orchestrator
	KeyOrchestratorAddress = "KeyOrchestratorAddress"
	// ValidatorByEthAddressKey indexes ethereum addresses
	// i.e. 0xAb5801a7D398351b8bE11C439e05C5B3259aeC9B
	ValidatorByEthAddressKey = "ValidatorByEthAddressKey"
)

// GetValsetConfirmKey returns the following key format
// prefix   nonce                    validator-address
// [0x0][0 0 0 0 0 0 0 1][gravity1ahx7f8wyertuus9r20284ej0asrs085ceqtfnm]
// FIXME: For our keys, should they be `gravity1ahx7f8wyertuus9r20284ej0asrs085ceqtfnm` or `qgb1ahx7f8wyertuus9r20284ej0asrs085ceqtfnm`?
func GetValsetConfirmKey(nonce uint64, validator sdk.AccAddress) string {
	if err := sdk.VerifyAddressFormat(validator); err != nil {
		panic(sdkerrors.Wrap(err, "invalid validator address"))
	}
	return ValsetConfirmKey + ConvertByteArrToString(UInt64Bytes(nonce)) + string(validator.Bytes())
}

func ConvertByteArrToString(value []byte) string {
	var ret strings.Builder
	for i := 0; i < len(value); i++ {
		ret.WriteString(string(value[i]))
	}
	return ret.String()
}

// GetOrchestratorAddressKey returns the following key format
// prefix
// [0xe8][gravity1ahx7f8wyertuus9r20284ej0asrs085ceqtfnm]
func GetOrchestratorAddressKey(orc sdk.AccAddress) string {
	if err := sdk.VerifyAddressFormat(orc); err != nil {
		panic(sdkerrors.Wrap(err, "invalid orchestrator address"))
	}
	return KeyOrchestratorAddress + string(orc.Bytes())
}

// GetEthAddressByValidatorKey returns the following key format
// prefix              cosmos-validator
// [0x0][gravityvaloper1ahx7f8wyertuus9r20284ej0asrs085ceqtfnm]
func GetEthAddressByValidatorKey(validator sdk.ValAddress) string {
	if err := sdk.VerifyAddressFormat(validator); err != nil {
		panic(sdkerrors.Wrap(err, "invalid validator address"))
	}
	return EthAddressByValidatorKey + string(validator.Bytes())
}

// GetValidatorByEthAddressKey returns the following key format
// prefix              cosmos-validator
// [0xf9][0xAb5801a7D398351b8bE11C439e05C5B3259aeC9B]
func GetValidatorByEthAddressKey(ethAddress EthAddress) string {
	return ValidatorByEthAddressKey + string([]byte(ethAddress.GetAddress()))
}
