package types

import (
	"strconv"
	"strings"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	// ModuleName is the name of the module
	ModuleName = "qgb"

	// StoreKey to be used when creating the KVStore
	StoreKey = ModuleName

	// RouterKey is the module name router key
	RouterKey = ModuleName

	// QuerierRoute to be used for querier msgs
	QuerierRoute = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_qgb"
)

const (
	// ValsetRequestKey indexes valset requests by nonce
	AttestationRequestKey = "AttestationRequestKey"

	// ValsetConfirmKey indexes valset confirmations by nonce and the validator account address
	// i.e celes1ahx7f8wyertuus9r20284ej0asrs085ceqtfnm
	ValsetConfirmKey = "ValsetConfirmKey"
	// DataCommitmentConfirmKey indexes data commitment confirmations by commitment and the validator account address
	DataCommitmentConfirmKey = "DataCommitmentConfirmKey"
	// EthAddressByValidatorKey indexes cosmos validator account addresses
	// i.e. celes1ahx7f8wyertuus9r20284ej0asrs085ceqtfnm
	EthAddressByValidatorKey = "EthAddressValidatorKey"
	// KeyOrchestratorAddress indexes the validator keys for an orchestrator
	KeyOrchestratorAddress = "KeyOrchestratorAddress"
	// ValidatorByEthAddressKey indexes ethereum addresses
	// i.e. 0xAb5801a7D398351b8bE11C439e05C5B3259aeC9B
	ValidatorByEthAddressKey = "ValidatorByEthAddressKey"

	// LastUnBondingBlockHeight indexes the last validator unbonding block height
	LastUnBondingBlockHeight = "LastUnBondingBlockHeight"

	// LatestAttestationtNonce indexes the latest attestation request nonce
	LatestAttestationtNonce = "LatestAttestationNonce"
)

// GetValsetConfirmKey returns the following key format
// prefix   nonce                    validator-address
// [0x0][0 0 0 0 0 0 0 1][celes1ahx7f8wyertuus9r20284ej0asrs085ceqtfnm]
func GetValsetConfirmKey(nonce uint64, validator sdk.AccAddress) string {
	if err := sdk.VerifyAddressFormat(validator); err != nil {
		panic(sdkerrors.Wrap(err, "invalid validator address"))
	}
	return ValsetConfirmKey + ConvertByteArrToString(UInt64Bytes(nonce)) + string(validator.Bytes())
}

// GetAttestationKey returns the following key format
// prefix    nonce
// [0x0][0 0 0 0 0 0 0 1]
func GetAttestationKey(nonce uint64) string {
	return AttestationRequestKey + string(UInt64Bytes(nonce))
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
// [0xe8][celes1ahx7f8wyertuus9r20284ej0asrs085ceqtfnm]
func GetOrchestratorAddressKey(orc sdk.AccAddress) string {
	if err := sdk.VerifyAddressFormat(orc); err != nil {
		panic(sdkerrors.Wrap(err, "invalid orchestrator address"))
	}
	return KeyOrchestratorAddress + string(orc.Bytes())
}

// GetEthAddressByValidatorKey returns the following key format
// prefix              cosmos-validator
// [0x0][celesvaloper1ahx7f8wyertuus9r20284ej0asrs085ceqtfnm]
func GetEthAddressByValidatorKey(validator sdk.ValAddress) string {
	if err := sdk.VerifyAddressFormat(validator); err != nil {
		panic(sdkerrors.Wrap(err, "invalid validator address"))
	}
	return EthAddressByValidatorKey + string(validator.Bytes())
}

// GetValidatorByEthAddressKey returns the following key format
// prefix              cosmos-validator
// [0xf9][0xAb5801a7D398351b8bE11C439e05C5B3259aeC9B]
func GetValidatorByEthAddressKey(ethAddress stakingtypes.EthAddress) string {
	return ValidatorByEthAddressKey + string([]byte(ethAddress.GetAddress()))
}

// GetDataCommitmentConfirmKey returns the following key format
// prefix  endBlock         beginBlock       validator-address
// [0x0][0 0 0 0 0 0 0 1][0 0 0 0 0 0 0 1][celes1ahx7f8wyertuus9r20284ej0asrs085ceqtfnm]
func GetDataCommitmentConfirmKey(endBlock uint64, beginBlock uint64, validator sdk.AccAddress) string {
	if err := sdk.VerifyAddressFormat(validator); err != nil {
		panic(sdkerrors.Wrap(err, "invalid validator address"))
	}
	return DataCommitmentConfirmKey +
		strconv.FormatInt(int64(endBlock), 16) +
		strconv.FormatInt(int64(beginBlock), 16) +
		string(validator.Bytes())
}
