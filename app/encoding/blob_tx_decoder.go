package encoding

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	coretypes "github.com/celestiaorg/go-square/v2/tx"
)

func blobTxDecoder(decoder sdk.TxDecoder) sdk.TxDecoder {
	return func(txBytes []byte) (sdk.Tx, error) {
		blobTx, isBlobTx, err := coretypes.UnmarshalBlobTx(txBytes)
		if err != nil {
			return nil, err
		}
		if isBlobTx {
			return decoder(blobTx.Tx)
		}
		return decoder(txBytes)
	}
}
