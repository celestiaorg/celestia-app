package types

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
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

// ParsePayForFibreMsg returns the MsgPayForFibre carried by plain Cosmos SDK Tx
// bytes, or false when there is none or it is malformed. It decodes with plain
// proto, so it is safe to call concurrently and needs no SDK tx decoder.
func ParsePayForFibreMsg(txBytes []byte) (*MsgPayForFibre, bool) {
	msg, isFibreTx, err := parsePayForFibre(txBytes)
	if !isFibreTx || err != nil {
		return nil, false
	}
	return msg, true
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
	// BlobTx bytes are wire-compatible with TxRaw and would decode
	// successfully below, so short-circuit them first: a BlobTx is never a
	// fibre tx, even one crafted so that its inner tx carries a
	// MsgPayForFibre. This also avoids copying blob payloads into throwaway
	// TxRaw buffers.
	if _, isBlobTx, _ := squaretx.UnmarshalBlobTx(txBytes); isBlobTx {
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
