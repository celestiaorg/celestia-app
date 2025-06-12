package encoding

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	coretypes "github.com/tendermint/tendermint/types"
)

func blobTxDecoder(decoder sdk.TxDecoder) sdk.TxDecoder {
	return func(txBytes []byte) (sdk.Tx, error) {
		if blobTx, isBlobTx := coretypes.UnmarshalBlobTx(txBytes); isBlobTx {
			return decoder(blobTx.Tx)
		}
		return decoder(txBytes)
	}
}
