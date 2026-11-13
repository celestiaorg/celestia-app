package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
)

// gasPerAdditionalSDKMessage is the gas limit a tx must declare for every
// message it counts against MaxSDKMessages beyond the first, for this node to
// admit it to the mempool or include it in a proposal.
//
// A MsgExec with empty inner messages, or an ICA packet with junk entries, can
// count hundreds of messages for a small fee and keep every other SDK tx out of
// the block. Fees are paid on the gas limit, so this floor prices each counted
// message. Honest messages use more than this much gas, so their gas limit
// already covers it.
//
// This is a local mempool and proposal policy, not a validity rule:
// ProcessProposal and FinalizeBlock still accept such a tx.
const gasPerAdditionalSDKMessage uint64 = 10_000

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
