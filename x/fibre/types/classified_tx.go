package types

import (
	"fmt"

	square "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	squaretx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/cosmos/btcutil/bech32"
	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
)

// MsgPayForFibreTypeURL is the Cosmos SDK message type URL for MsgPayForFibre.
const MsgPayForFibreTypeURL = "/celestia.fibre.v1.MsgPayForFibre"

// ClassifyTxs classifies raw transactions for square construction, marking
// pay-for-fibre transactions and synthesizing their system blobs. go-square
// no longer decodes Cosmos SDK transactions, so the application classifies
// them and passes the result in.
func ClassifyTxs(txs [][]byte) ([]square.ClassifiedTx, error) {
	classified := make([]square.ClassifiedTx, len(txs))
	for i, rawTx := range txs {
		fibreTx, isFibreTx, err := TryParseFibreTx(rawTx)
		if err != nil {
			return nil, fmt.Errorf("parsing fibre tx at index %d: %w", i, err)
		}
		if !isFibreTx {
			classified[i] = square.NewClassifiedTx(rawTx)
			continue
		}
		classified[i], err = square.NewClassifiedFibreTx(fibreTx)
		if err != nil {
			return nil, fmt.Errorf("classifying fibre tx at index %d: %w", i, err)
		}
	}
	return classified, nil
}

// TryParseFibreTx attempts to detect a MsgPayForFibre message inside plain
// Cosmos SDK Tx bytes and synthesize the corresponding FibreTx.
//
// Returns:
//   - (nil, false, nil): txBytes do not contain a MsgPayForFibre (not a fibre tx).
//   - (nil, true, err): txBytes contain a MsgPayForFibre but it is malformed.
//   - (ft, true, nil): successfully parsed and synthesized a FibreTx.
func TryParseFibreTx(txBytes []byte) (fibreTx *squaretx.FibreTx, isFibreTx bool, err error) {
	var sdkTx cosmostx.Tx
	// Not returning an error here because BlobTx bytes fail to unmarshal into
	// cosmos.tx.v1beta1.Tx and callers pass BlobTx bytes through here.
	if err := sdkTx.Unmarshal(txBytes); err != nil {
		return nil, false, nil
	}
	if sdkTx.Body == nil || len(sdkTx.Body.Messages) == 0 {
		return nil, false, nil
	}

	anyMsg := sdkTx.Body.Messages[0]
	if anyMsg.TypeUrl != MsgPayForFibreTypeURL {
		return nil, false, nil
	}

	var msg MsgPayForFibre
	if err := msg.Unmarshal(anyMsg.Value); err != nil {
		return nil, true, fmt.Errorf("unmarshalling MsgPayForFibre: %w", err)
	}

	systemBlob, err := msg.SystemBlob()
	if err != nil {
		return nil, true, err
	}

	return &squaretx.FibreTx{
		Tx:         txBytes,
		SystemBlob: systemBlob,
	}, true, nil
}

// SystemBlob synthesizes the share version two system blob that represents
// this message's payment promise in the data square.
func (msg *MsgPayForFibre) SystemBlob() (*share.Blob, error) {
	ns, err := share.NewNamespaceFromBytes(msg.PaymentPromise.Namespace)
	if err != nil {
		return nil, fmt.Errorf("invalid namespace in MsgPayForFibre: %w", err)
	}

	// Decode without enforcing a prefix so classification does not depend on
	// the global SDK bech32 config being set.
	_, signerBytes, err := bech32.DecodeToBase256(msg.Signer)
	if err != nil {
		return nil, fmt.Errorf("decoding signer address in MsgPayForFibre: %w", err)
	}

	systemBlob, err := share.NewV2Blob(ns, msg.PaymentPromise.BlobVersion, msg.PaymentPromise.Commitment, signerBytes)
	if err != nil {
		return nil, fmt.Errorf("creating system blob for MsgPayForFibre: %w", err)
	}
	return systemBlob, nil
}
