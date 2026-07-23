package encoding

import (
	apperr "github.com/celestiaorg/celestia-app/v10/app/errors"
	blobtypes "github.com/celestiaorg/celestia-app/v10/x/blob/types"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func indexWrapperDecoder(decoder sdk.TxDecoder) sdk.TxDecoder {
	return func(txBytes []byte) (sdk.Tx, error) {
		indexWrapper, isIndexWrapper := coretypes.UnmarshalIndexWrapper(txBytes)
		if !isIndexWrapper {
			return decoder(txBytes)
		}
		sdkTx, err := decoder(indexWrapper.Tx)
		if err != nil {
			return nil, err
		}
		msgs := sdkTx.GetMsgs()
		// IndexWrapper txs must contain exactly one MsgPayForBlobs (mirrors the
		// invariant enforced by ValidateBlobTxSkipCommitment in x/blob/types/blob_tx.go).
		if len(msgs) != 1 {
			return nil, apperr.ErrNonPFBIndexWrapper
		}
		if _, ok := msgs[0].(*blobtypes.MsgPayForBlobs); !ok {
			return nil, apperr.ErrNonPFBIndexWrapper
		}
		return sdkTx, nil
	}
}
