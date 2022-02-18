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
