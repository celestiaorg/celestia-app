package encoding

import (
	"github.com/celestiaorg/go-square/v2/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func blobTxDecoder(decoder sdk.TxDecoder) sdk.TxDecoder {
	return func(txBytes []byte) (sdk.Tx, error) {
		blobTx, isBlobTx, err := tx.UnmarshalBlobTx(txBytes)
		if isBlobTx {
			if err != nil {
				return nil, err
			}
			return decoder(blobTx.Tx)
		}
		return decoder(txBytes)
	}
}
