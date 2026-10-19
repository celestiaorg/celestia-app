package types

import (
	"bytes"
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	square "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	squaretx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/cosmos/btcutil/bech32"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
)

// MsgPayForFibreTypeURL is the Cosmos SDK message type URL for MsgPayForFibre.
const MsgPayForFibreTypeURL = "/celestia.fibre.v1.MsgPayForFibre"

// msgPayForFibreTypeURL is MsgPayForFibreTypeURL as bytes, for the substring
// prefilter in parsePayForFibre.
var msgPayForFibreTypeURL = []byte(MsgPayForFibreTypeURL)

// ClassifyTxs classifies raw transactions for square construction, marking
// pay-for-fibre transactions and synthesizing their system blobs. go-square
// no longer decodes Cosmos SDK transactions, so the application classifies
// them and passes the result in.
func ClassifyTxs(txs [][]byte) ([]square.ClassifiedTx, error) {
	return ClassifyTxsDecoded(txs, nil, nil)
}

// ClassifyTxsDecoded is ClassifyTxs for a caller that already decoded the txs.
// blobTxs and decoded are index aligned with txs; nil entries fall back to parsing.
// blobTxs[i] is trusted as the decode of txs[i]: a wrong one changes the data root.
func ClassifyTxsDecoded(txs [][]byte, blobTxs []*squaretx.BlobTx, decoded []*DecodedPayForFibre) ([]square.ClassifiedTx, error) {
	if len(blobTxs) != 0 && len(blobTxs) != len(txs) {
		return nil, fmt.Errorf("blob txs (%d) are not index aligned with txs (%d)", len(blobTxs), len(txs))
	}
	if len(decoded) != 0 && len(decoded) != len(txs) {
		return nil, fmt.Errorf("decoded pay-for-fibre txs (%d) are not index aligned with txs (%d)", len(decoded), len(txs))
	}
	hasBlobTxs, hasDecoded := len(blobTxs) != 0, len(decoded) != 0

	classified := make([]square.ClassifiedTx, len(txs))
	for i, rawTx := range txs {
		var (
			fibreTx   *squaretx.FibreTx
			isFibreTx bool
			err       error
		)
		switch {
		case hasBlobTxs && blobTxs[i] != nil:
			// A blob tx is never a pay-for-fibre tx; hand go-square the
			// decode the caller already has.
			classified[i], err = square.NewClassifiedBlobTx(rawTx, blobTxs[i])
			if err != nil {
				return nil, fmt.Errorf("classifying blob tx at index %d: %w", i, err)
			}
			continue
		case hasDecoded && decoded[i] != nil:
			isFibreTx = true
			fibreTx, err = decoded[i].FibreTx()
		default:
			fibreTx, isFibreTx, err = TryParseFibreTx(rawTx)
		}
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

// SigCacheKey derives the signature cache key for msg's certificate: the
// message without its signer, so the key covers exactly the inputs to signature
// verification. Proto encoding length-prefixes every field, so distinct
// certificates cannot collide.
func (msg *MsgPayForFibre) SigCacheKey() (sigcache.Key, error) {
	certificate := MsgPayForFibre{
		PaymentPromise:      msg.PaymentPromise,
		ValidatorSignatures: msg.ValidatorSignatures,
	}
	bz, err := certificate.Marshal()
	if err != nil {
		return sigcache.Key{}, err
	}
	return sigcache.NewKey(sigcache.PffCertificate, bz), nil
}

// PayForFibreMsg returns tx's MsgPayForFibre, or nil when tx is not a
// single-message pay-for-fibre transaction. A MsgPayForFibre must be the only
// message in its tx, so anything else is not one.
func PayForFibreMsg(tx sdk.Tx) *MsgPayForFibre {
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return nil
	}
	msg, _ := msgs[0].(*MsgPayForFibre)
	return msg
}

// ParsePayForFibreTx decodes the single MsgPayForFibre carried by txBytes. The
// bool reports whether txBytes is a fibre tx; an error means it is one but malformed.
func ParsePayForFibreTx(txBytes []byte) (*MsgPayForFibre, bool, error) {
	return parsePayForFibre(txBytes)
}

// IsPayForFibreTx reports whether txBytes are a well formed pay-for-fibre
// transaction, straight from the bytes and without the SDK tx decoder. A
// malformed MsgPayForFibre is not one: it would be rejected rather than
// admitted, so a caller may not take a shortcut reserved for admitted bytes.
func IsPayForFibreTx(txBytes []byte) bool {
	msg, isPFF, err := parsePayForFibre(txBytes)
	return isPFF && err == nil && msg != nil
}

// TryParseFibreTx attempts to detect a MsgPayForFibre message inside plain
// Cosmos SDK Tx bytes and synthesize the corresponding FibreTx.
//
// Returns:
//   - (nil, false, nil): txBytes do not contain a MsgPayForFibre (not a fibre tx).
//   - (nil, true, err): txBytes contain a MsgPayForFibre but it is malformed.
//   - (ft, true, nil): successfully parsed and synthesized a FibreTx.
func TryParseFibreTx(txBytes []byte) (fibreTx *squaretx.FibreTx, isFibreTx bool, err error) {
	msg, isFibreTx, err := parsePayForFibre(txBytes)
	if !isFibreTx || err != nil {
		return nil, isFibreTx, err
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

// parsePayForFibre decodes the single MsgPayForFibre carried by txBytes. The
// second result reports whether txBytes is a fibre tx at all; an error means it
// is one but the message is malformed.
func parsePayForFibre(txBytes []byte) (*MsgPayForFibre, bool, error) {
	// A MsgPayForFibre's type URL appears verbatim in the transaction bytes:
	// it is a string field of the Any inside TxBody, and body_bytes is an
	// opaque byte field of TxRaw, so both are subranges of txBytes. Bytes that
	// do not contain it therefore cannot carry one. Checking first keeps every
	// caller that scans a whole block - PreBlocker's warming pass most of all -
	// from unmarshalling transactions that are not candidates, which for a blob
	// tx means copying every blob payload.
	if !bytes.Contains(txBytes, msgPayForFibreTypeURL) {
		return nil, false, nil
	}

	// Decode the way the SDK's tx decoder does: the outer TxRaw carries
	// body_bytes as an opaque scalar (a repeated occurrence resolves to the
	// last one), which is then unmarshalled into a TxBody. Decoding into the
	// embedded-message cosmostx.Tx instead would merge duplicate body fields
	// and could disagree with the SDK about what a transaction contains.
	//
	// Not returning an error on unmarshal failures because callers pass
	// non-SDK transaction bytes through here.
	var raw cosmostx.TxRaw
	if err := raw.Unmarshal(txBytes); err != nil {
		return nil, false, nil
	}
	var body cosmostx.TxBody
	if err := body.Unmarshal(raw.BodyBytes); err != nil {
		return nil, false, nil
	}
	// A fibre tx contains exactly one message, matching
	// validatePayForFibreTxShape.
	if len(body.Messages) != 1 {
		return nil, false, nil
	}

	anyMsg := body.Messages[0]
	if anyMsg.TypeUrl != MsgPayForFibreTypeURL {
		return nil, false, nil
	}

	// BlobTx bytes are wire-compatible with TxRaw and can reach here, so rule
	// them out before accepting: a BlobTx is never a fibre tx, even one
	// crafted so that its inner tx carries a MsgPayForFibre. HasBlobTxTypeID is
	// exactly UnmarshalBlobTx's own "is this a BlobTx" answer - that function
	// reports true on every path after the type id matches - without rebuilding
	// and re-marshalling the blobs to reach it.
	if squaretx.HasBlobTxTypeID(txBytes) {
		return nil, false, nil
	}

	var msg MsgPayForFibre
	if err := msg.Unmarshal(anyMsg.Value); err != nil {
		return nil, true, fmt.Errorf("unmarshalling MsgPayForFibre: %w", err)
	}
	return &msg, true, nil
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
