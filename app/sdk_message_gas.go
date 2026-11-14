package app

import (
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
)

// validateSDKMessageGas returns an error if the gas limit of tx, which counts
// msgCount messages against MaxSDKMessages, is below
// GasPerAdditionalSDKMessage per message after the first.
func validateSDKMessageGas(tx sdk.Tx, msgCount int) error {
	if msgCount <= 1 {
		return nil
	}
	// msgCount is bounded by the tx size, so this cannot overflow.
	required := uint64(msgCount-1) * appconsts.GasPerAdditionalSDKMessage
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
