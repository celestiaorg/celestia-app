package encoding

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	coretypes "github.com/tendermint/tendermint/types"
)

func MalleatedTxDecoder(dec sdk.TxDecoder) sdk.TxDecoder {
	return func(txBytes []byte) (sdk.Tx, error) {
		if malleatedTx, has := coretypes.UnwrapMalleatedTx(txBytes); has {
			return dec(malleatedTx.Tx)
		}
		return dec(txBytes)
	}
}
