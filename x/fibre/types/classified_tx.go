package types

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	square "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	squaretx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/cosmos/btcutil/bech32"
	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
	"google.golang.org/protobuf/encoding/protowire"
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
	msg, _, isFibreTx, err := parsePayForFibre(txBytes)
	if !isFibreTx || err != nil {
		return nil, false
	}
	return msg, true
}

// ParsePayForFibreTx returns the message, the declared gas limit, whether
// txBytes is a fibre tx, and a decode error. The gas limit is zero when the
// auth info cannot be read.
func ParsePayForFibreTx(txBytes []byte) (*MsgPayForFibre, uint64, bool, error) {
	return parsePayForFibre(txBytes)
}

// TryParseFibreTx attempts to detect a MsgPayForFibre message inside plain
// Cosmos SDK Tx bytes and synthesize the corresponding FibreTx.
//
// Returns:
//   - (nil, false, nil): txBytes do not contain a MsgPayForFibre (not a fibre tx).
//   - (nil, true, err): txBytes contain a MsgPayForFibre but it is malformed.
//   - (ft, true, nil): successfully parsed and synthesized a FibreTx.
func TryParseFibreTx(txBytes []byte) (fibreTx *squaretx.FibreTx, isFibreTx bool, err error) {
	msg, _, isFibreTx, err := parsePayForFibre(txBytes)
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
// parsePayForFibre also reports the declared gas limit, so callers that need it
// do not have to unmarshal the transaction a second time.
func parsePayForFibre(txBytes []byte) (*MsgPayForFibre, uint64, bool, error) {
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
		return nil, 0, false, nil
	}
	var body cosmostx.TxBody
	if err := body.Unmarshal(raw.BodyBytes); err != nil {
		return nil, 0, false, nil
	}
	// A fibre tx contains exactly one message, matching
	// validatePayForFibreTxShape.
	if len(body.Messages) != 1 {
		return nil, 0, false, nil
	}

	anyMsg := body.Messages[0]
	if anyMsg.TypeUrl != MsgPayForFibreTypeURL {
		return nil, 0, false, nil
	}

	// BlobTx bytes are wire-compatible with TxRaw and can reach here, so rule
	// them out before accepting: a BlobTx is never a fibre tx, even one
	// crafted so that its inner tx carries a MsgPayForFibre. The check is last
	// because it is the expensive one - it decodes every blob payload - and
	// only a tx that already looks like a PayForFibre can need it. Ordinary
	// blob traffic leaves after the type URL comparison above.
	if _, isBlobTx, _ := squaretx.UnmarshalBlobTx(txBytes); isBlobTx {
		return nil, 0, false, nil
	}

	var msg MsgPayForFibre
	if err := msg.Unmarshal(anyMsg.Value); err != nil {
		return nil, 0, true, fmt.Errorf("unmarshalling MsgPayForFibre: %w", err)
	}

	return &msg, gasLimitFromAuthInfo(raw.AuthInfoBytes), true, nil
}

// Field numbers in cosmos.tx.v1beta1, used to read the declared gas without
// materialising the messages around it.
const (
	authInfoFeeField = 2 // AuthInfo.fee
	feeGasLimitField = 2 // Fee.gas_limit
)

// gasLimitFromAuthInfo reads auth_info.fee.gas_limit off the wire, without
// decoding AuthInfo: these are attacker-controlled bytes and a full decode
// allocates many times what it is handed. Zero means absent or malformed.
func gasLimitFromAuthInfo(authInfoBytes []byte) uint64 {
	// Protobuf merges repeated occurrences of a singular message field, so
	// gas_limit is the last value explicitly present across all `fee`
	// occurrences, not the value in the last occurrence. Reading only the last
	// one reports zero for a transaction the SDK sees as well funded, and a
	// zero here makes the pre-verification pass skip it - a switch anyone
	// could use to force their certificates onto the sequential path.
	var gasLimit uint64
	rest := authInfoBytes
	for {
		fee, remainder, found := nextProtoSubMessage(rest, authInfoFeeField)
		if !found {
			return gasLimit
		}
		rest = remainder
		gasLimit = mergeFeeGasLimit(fee, gasLimit)
	}
}

// mergeFeeGasLimit returns the gas limit after merging one `fee` occurrence
// into a value already merged from earlier occurrences: a gas_limit present in
// this occurrence replaces it, an absent one leaves it.
func mergeFeeGasLimit(fee []byte, gasLimit uint64) uint64 {
	for len(fee) > 0 {
		num, typ, n := protowire.ConsumeTag(fee)
		if n < 0 {
			return 0
		}
		fee = fee[n:]
		// proto3 has no groups, and walking them would recurse as deep as the
		// input is long. Reject rather than walk, as countProtoMsgs does.
		if typ == protowire.StartGroupType || typ == protowire.EndGroupType {
			return 0
		}
		if num == feeGasLimitField && typ == protowire.VarintType {
			value, n := protowire.ConsumeVarint(fee)
			if n < 0 {
				return 0
			}
			gasLimit = value
			fee = fee[n:]
			continue
		}
		if n = protowire.ConsumeFieldValue(num, typ, fee); n < 0 {
			return 0
		}
		fee = fee[n:]
	}
	return gasLimit
}

// nextProtoSubMessage returns the bytes of the first occurrence of field and
// what follows it, so a caller can walk every occurrence. A decoder merges
// repeated occurrences of a singular message field rather than keeping only
// the last, which is why callers must not stop at the first one.
func nextProtoSubMessage(bz []byte, field protowire.Number) (value, rest []byte, found bool) {
	for len(bz) > 0 {
		num, typ, n := protowire.ConsumeTag(bz)
		if n < 0 {
			return nil, nil, false
		}
		bz = bz[n:]
		if typ == protowire.StartGroupType || typ == protowire.EndGroupType {
			return nil, nil, false
		}
		if num == field && typ == protowire.BytesType {
			if value, n = protowire.ConsumeBytes(bz); n < 0 {
				return nil, nil, false
			}
			return value, bz[n:], true
		}
		if n = protowire.ConsumeFieldValue(num, typ, bz); n < 0 {
			return nil, nil, false
		}
		bz = bz[n:]
	}
	return nil, nil, false
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
