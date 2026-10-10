package app

import (
	"bytes"
	"unicode/utf8"

	addresscodec "cosmossdk.io/core/address"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
)

// canonicalProposalSigner avoids materializing a second protobuf representation
// solely to read the signer of an immutable canonical single-message PFF.
type canonicalProposalSigner struct {
	proposalViewTx
	signers [][]byte
}

func (tx canonicalProposalSigner) GetSigners() ([][]byte, error) { return tx.signers, nil }
func (tx canonicalProposalSigner) FeePayer() []byte              { return tx.signers[0] }
func (tx canonicalProposalSigner) ValidateBasic() error {
	// The constructor requires one signature and exactly one signer, with no
	// separate fee payer. Keep every remaining SDK transaction check.
	return tx.GetProtoTx().ValidateBasic()
}

func cacheCanonicalProposalSigner(tx proposalViewTx, codec addresscodec.Codec) proposalViewTx {
	if codec == nil {
		return tx
	}
	raw := tx.GetProtoTx()
	if raw == nil || raw.Body == nil || len(raw.Body.Messages) != 1 || len(raw.Signatures) != 1 || raw.AuthInfo == nil || raw.AuthInfo.Fee == nil || raw.AuthInfo.Fee.Payer != "" {
		return tx
	}
	anyMsg := raw.Body.Messages[0]
	if anyMsg == nil || anyMsg.TypeUrl != fibretypes.MsgPayForFibreTypeURL {
		return tx
	}
	messages := tx.GetMsgs()
	if len(messages) != 1 {
		return tx
	}
	msg, ok := messages[0].(*fibretypes.MsgPayForFibre)
	if !ok || msg == nil || !utf8.ValidString(msg.Signer) || !utf8.ValidString(msg.PaymentPromise.ChainId) {
		return tx
	}
	// Equality excludes unknown fields, duplicate fields, noncanonical varints,
	// and earlier invalid UTF-8 strings overwritten by later field occurrences.
	canonical, err := msg.Marshal()
	if err != nil || !bytes.Equal(canonical, anyMsg.Value) {
		return tx
	}
	signer, err := codec.StringToBytes(msg.Signer)
	if err != nil {
		return tx
	}
	return canonicalProposalSigner{proposalViewTx: tx, signers: [][]byte{signer}}
}
