package encoding

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	coretypes "github.com/tendermint/tendermint/types"
)

func indexWrapperDecoder(decoder sdk.TxDecoder) sdk.TxDecoder {
	return func(txBytes []byte) (sdk.Tx, error) {
		if indexWrapper, isIndexWrapper := coretypes.UnmarshalIndexWrapper(txBytes); isIndexWrapper {
			return decoder(indexWrapper.Tx)
		}
		if blobTx, isBlobTx := coretypes.UnmarshalBlobTx(txBytes); isBlobTx {
			return decoder(blobTx.Tx)
		}
		return decoder(txBytes)
	}
}
