package app

import (
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	sdkante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

type proposalViewTx interface {
	authsigning.Tx
	authsigning.V2AdaptableTx
	sdkante.HasExtensionOptionsTx
	GetProtoTx() *txtypes.Tx
}

type cachedProposalViews struct {
	proposalViewTx
	messages   []sdk.Msg
	signatures []signing.SignatureV2
	keys       []cryptotypes.PubKey
}

func (tx cachedProposalViews) GetMsgs() []sdk.Msg { return tx.messages }
func (tx cachedProposalViews) GetSignaturesV2() ([]signing.SignatureV2, error) {
	return tx.signatures, nil
}
func (tx cachedProposalViews) GetPubKeys() ([]cryptotypes.PubKey, error) { return tx.keys, nil }

// cacheProposalViews reuses immutable views of decoded single-signer PFFs.
// Unsupported or malformed transactions retain their original implementation.
func cacheProposalViews(tx sdk.Tx) (result sdk.Tx) {
	result = tx
	defer func() { _ = recover() }()
	original, ok := tx.(proposalViewTx)
	if !ok {
		return result
	}
	signatures, err := original.GetSignaturesV2()
	if err != nil || len(signatures) != 1 {
		return result
	}
	if _, ok := signatures[0].Data.(*signing.SingleSignatureData); !ok {
		return result
	}
	result = cachedProposalViews{
		proposalViewTx: original, messages: original.GetMsgs(), signatures: signatures,
		keys: []cryptotypes.PubKey{signatures[0].PubKey},
	}
	return result
}
