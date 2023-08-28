package encoding

import (
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func indexWrapperDecoder(dec sdk.TxDecoder) sdk.TxDecoder {
	return func(txBytes []byte) (sdk.Tx, error) {
		if indexWrapper, isIndexWrapper := coretypes.UnmarshalIndexWrapper(txBytes); isIndexWrapper {
			return dec(indexWrapper.Tx)
		}
		return dec(txBytes)
	}
}
