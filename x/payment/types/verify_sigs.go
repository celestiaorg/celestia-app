package types

import (
	"errors"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

var errorSinglSignerExpected = errors.New("only a single signer is supported")

// VerifySig checks that the signature over the provided transaction is valid using the provided signer data.
func VerifySig(signerData authsigning.SignerData, txConfig client.TxConfig, authTx authsigning.Tx) (bool, error) {
	signBytes, err := txConfig.SignModeHandler().GetSignBytes(signing.SignMode_SIGN_MODE_DIRECT, signerData, authTx)
	if err != nil {
		return false, err
	}

	sigs, err := authTx.GetSignaturesV2()
	if err != nil {
		return false, err
	}
	if len(sigs) != 1 {
		return false, errorSinglSignerExpected
	}

	sigData := sigs[0].Data

	rawSig, ok := sigData.(*signing.SingleSignatureData)
	if !ok {
		return false, errorSinglSignerExpected
	}

	return signerData.PubKey.VerifySignature(signBytes, rawSig.Signature), nil
}

// VerifyPFDSig verifies the signature in a MsgWirePayForData by going through
// the malleation process. Returns true if the signature is valid.
func VerifyPFDSig(signerData authsigning.SignerData, txConfig client.TxConfig, wirePFDTx authsigning.Tx) (bool, error) {
	wirePFDMsg, err := ExtractMsgWirePayForData(wirePFDTx)
	if err != nil {
		return false, err
	}

	// go through the entire malleation process as if this tx was being included in a block.
	_, pfd, sig, err := ProcessWirePayForData(wirePFDMsg, appconsts.MinSquareSize)
	if err != nil {
		return false, err
	}

	// create the malleated MsgPayForData tx by using auth data from the original tx
	pfdTx, err := BuildPayForDataTxFromWireTx(wirePFDTx, txConfig.NewTxBuilder(), sig, pfd)
	if err != nil {
		return false, err
	}

	valid, err := VerifySig(signerData, txConfig, pfdTx)
	if err != nil || !valid {
		return false, err
	}

	return true, nil
}
