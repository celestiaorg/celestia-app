package encoding

import (
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func indexWrapperDecoder(decoder sdk.TxDecoder) sdk.TxDecoder {
	return func(txBytes []byte) (sdk.Tx, error) {
		if indexWrapper, isIndexWrapper := coretypes.UnmarshalIndexWrapper(txBytes); isIndexWrapper {
			return decoder(indexWrapper.Tx)
		}
		return decoder(txBytes)
	}
}
