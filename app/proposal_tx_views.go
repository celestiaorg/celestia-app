package app

import (
	addresscodec "cosmossdk.io/core/address"
	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
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
	messages    []sdk.Msg
	signatures  []signing.SignatureV2
	keys        []cryptotypes.PubKey
	payload     sigcache.Key
	signingData *txsigning.TxData
}

func (tx cachedProposalViews) GetMsgs() []sdk.Msg                 { return tx.messages }
func (tx cachedProposalViews) GetSigningTxData() txsigning.TxData { return *tx.signingData }
func (tx cachedProposalViews) GetSignaturesV2() ([]signing.SignatureV2, error) {
	return tx.signatures, nil
}
func (tx cachedProposalViews) GetPubKeys() ([]cryptotypes.PubKey, error) { return tx.keys, nil }

// SignaturePayloadKey is valid only for this immutable transaction's single
// signature and signing bytes. Other inputs take the ordinary hashing path.
func (tx cachedProposalViews) SignaturePayloadKey(data signing.SignatureData, signingTx txsigning.TxData) (sigcache.Key, bool) {
	return tx.payload, data == tx.signatures[0].Data && sameProposalBytes(tx.signingData.BodyBytes, signingTx.BodyBytes) && sameProposalBytes(tx.signingData.AuthInfoBytes, signingTx.AuthInfoBytes)
}

func sameProposalBytes(a, b []byte) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// cacheProposalViews reuses immutable views of decoded single-signer PFFs.
// Unsupported or malformed transactions retain their original implementation.
func cacheProposalViews(tx sdk.Tx, signerCodec addresscodec.Codec) (result sdk.Tx) {
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
	single, ok := signatures[0].Data.(*signing.SingleSignatureData)
	if !ok {
		return result
	}
	if single.SignMode == signing.SignMode_SIGN_MODE_DIRECT {
		original = cacheCanonicalProposalSigner(original, signerCodec)
	}
	signingTx := original.GetSigningTxData()
	result = cachedProposalViews{
		proposalViewTx: original, messages: original.GetMsgs(), signatures: signatures,
		keys:        []cryptotypes.PubKey{signatures[0].PubKey},
		signingData: &signingTx,
		payload:     sigcache.NewKey(sigcache.TxSignaturePayload, signingTx.BodyBytes, signingTx.AuthInfoBytes, single.Signature),
	}
	return result
}
