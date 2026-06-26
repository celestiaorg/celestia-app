package encoding

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ErrEmptyTx is returned when an empty (zero-length or nil) transaction is
// passed to the tx decoder. The default Cosmos SDK decoder returns a non-nil
// tx and no error for empty bytes, which can cause the state machine to panic
// downstream. See https://github.com/celestiaorg/celestia-app/issues/3175.
var ErrEmptyTx = errors.New("tx bytes are empty")

// rejectEmptyTxDecoder wraps a TxDecoder so that empty (zero-length or nil) tx
// bytes are rejected with an error instead of being decoded into a non-nil tx.
func rejectEmptyTxDecoder(decoder sdk.TxDecoder) sdk.TxDecoder {
	return func(txBytes []byte) (sdk.Tx, error) {
		if len(txBytes) == 0 {
			return nil, ErrEmptyTx
		}
		return decoder(txBytes)
	}
}
