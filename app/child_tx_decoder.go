package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	coretypes "github.com/tendermint/tendermint/types"
)

func MalleatedTxDecoder(dec sdk.TxDecoder) sdk.TxDecoder {
	return func(txBytes []byte) (sdk.Tx, error) {
		if _, childTx, has := coretypes.UnwrapMalleatedTx(txBytes); has {
			return dec(childTx)
		}
		return dec(txBytes)
	}
}
