package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
)

// gasPerAdditionalSDKMessage is the gas limit a tx must declare for each
// counted SDK message after the first, checked only at mempool admission and
// proposal construction. Unlike main, which charges 10,000 as real gas, this is
// only a floor that simulation does not include, so it stays below the cheapest
// honest message (about 6,000 gas).
const gasPerAdditionalSDKMessage uint64 = 5_000

// validateSDKMessageGas returns an error if the gas limit of tx, which counts
// msgCount messages against MaxSDKMessages, is below the floor above.
func validateSDKMessageGas(tx sdk.Tx, msgCount int) error {
	if msgCount <= 1 {
		return nil
	}
	// msgCount is bounded by the tx size, so this cannot overflow.
	required := uint64(msgCount-1) * gasPerAdditionalSDKMessage
	if gasLimit := txGasLimit(tx); gasLimit < required {
		return sdkerrors.ErrOutOfGas.Wrapf("tx counts %d SDK messages and needs a gas limit of at least %d, got %d", msgCount, required, gasLimit)
	}
	return nil
}

// txGasLimit returns the gas limit tx declares. It reads the fee through the
// nil safe proto getters because FeeTx.GetGas panics on a tx without AuthInfo
// or Fee. Such a tx reports zero here and ante rejects it.
func txGasLimit(tx sdk.Tx) uint64 {
	protoTx, ok := tx.(interface{ GetProtoTx() *txtypes.Tx })
	if !ok {
		return 0
	}
	return protoTx.GetProtoTx().GetAuthInfo().GetFee().GetGasLimit()
}
