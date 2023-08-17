package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	gethcommon "github.com/ethereum/go-ethereum/common"
)

// UInt64Bytes uses the SDK byte marshaling to encode a uint64.
func UInt64Bytes(n uint64) []byte {
	return sdk.Uint64ToBigEndian(n)
}

func DefaultEVMAddress(addr sdk.ValAddress) gethcommon.Address {
	return gethcommon.BytesToAddress(addr)
}
