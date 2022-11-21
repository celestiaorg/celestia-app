package types

import (
	"crypto/sha256"
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt"
	sdkclient "github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/tendermint/tendermint/crypto/merkle"
	coretypes "github.com/tendermint/tendermint/types"

	appshares "github.com/celestiaorg/celestia-app/pkg/shares"
)

const (
	URLMsgWirePayForBlob = "/blob.MsgWirePayForBlob"
	URLMsgPayForBlob     = "/blob.MsgPayForBlob"
	ShareSize            = appconsts.ShareSize
	SquareSize           = appconsts.MaxSquareSize
	NamespaceIDSize      = appconsts.NamespaceSize
)

var _ sdk.Msg = &MsgPayForBlob{}

// Route fullfills the sdk.Msg interface
func (msg *MsgPayForBlob) Route() string { return RouterKey }

// Type fullfills the sdk.Msg interface
func (msg *MsgPayForBlob) Type() string {
	return URLMsgPayForBlob
}

// ValidateBasic fullfills the sdk.Msg interface by performing stateless
// validity checks on the msg that also don't require having the actual message
func (msg *MsgPayForBlob) ValidateBasic() error {
	if err := ValidateMessageNamespaceID(msg.GetNamespaceId()); err != nil {
		return err
	}

	_, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return err
	}

	if len(msg.ShareCommitment) == 0 {
		return ErrEmptyShareCommitment
	}

	return nil
}

// GetSignBytes fullfills the sdk.Msg interface by reterning a deterministic set
// of bytes to sign over
func (msg *MsgPayForBlob) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

// GetSigners fullfills the sdk.Msg interface by returning the signer's address
func (msg *MsgPayForBlob) GetSigners() []sdk.AccAddress {
	address, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{address}
}

// BuildPayForBlobTxFromWireTx creates an authsigning.Tx using data from the original
// MsgWirePayForBlob sdk.Tx and the signature provided. This is used while processing
// the MsgWirePayForBlobs into Signed  MsgPayForBlob
func BuildPayForBlobTxFromWireTx(
	origTx authsigning.Tx,
	builder sdkclient.TxBuilder,
	signature []byte,
	msg *MsgPayForBlob,
) (authsigning.Tx, error) {
	err := builder.SetMsgs(msg)
	if err != nil {
		return nil, err
	}
	builder = InheritTxConfig(builder, origTx)

	origSigs, err := origTx.GetSignaturesV2()
	if err != nil {
		return nil, err
	}
	if len(origSigs) != 1 {
		return nil, fmt.Errorf("unexpected number of signatures: %d", len(origSigs))
	}

	newSig := signing.SignatureV2{
		PubKey: origSigs[0].PubKey,
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: signature,
		},
		Sequence: origSigs[0].Sequence,
	}

	err = builder.SetSignatures(newSig)
	if err != nil {
		return nil, err
	}

	return builder.GetTx(), nil
}

// CreateCommitment generates the commitment bytes for a given namespace and
// message using a namespace merkle tree and the rules described at [Message
// layout rationale] and [Non-interactive default rules].
//
// [Message layout rationale]: https://github.com/celestiaorg/celestia-specs/blob/e59efd63a2165866584833e91e1cb8a6ed8c8203/src/rationale/message_block_layout.md?plain=1#L12
// [Non-interactive default rules]: https://github.com/celestiaorg/celestia-specs/blob/e59efd63a2165866584833e91e1cb8a6ed8c8203/src/rationale/message_block_layout.md?plain=1#L36
func CreateCommitment(namespace []byte, message []byte, shareVersion uint8) ([]byte, error) {
	blob := coretypes.Blob{
		NamespaceID:  namespace,
		Data:         message,
		ShareVersion: shareVersion,
	}

	// split into shares that are length delimited and include the namespace in
	// each share
	shares, err := appshares.SplitMessages(0, nil, []coretypes.Blob{blob}, false)
	if err != nil {
		return nil, err
	}

	// the commitment is the root of a merkle mountain range with max tree size
	// equal to the minimum square size the message can be included in. See
	// https://github.com/celestiaorg/celestia-app/blob/fbfbf111bcaa056e53b0bc54d327587dee11a945/docs/architecture/adr-008-blocksize-independent-commitment.md
	minSquareSize := MsgMinSquareSize(len(message))
	treeSizes := merkleMountainRangeSizes(uint64(len(shares)), uint64(minSquareSize))
	leafSets := make([][][]byte, len(treeSizes))
	cursor := uint64(0)
	for i, treeSize := range treeSizes {
		leafSets[i] = appshares.ToBytes(shares[cursor : cursor+treeSize])
		cursor = cursor + treeSize
	}

	// create the commitments by pushing each leaf set onto an nmt
	subTreeRoots := make([][]byte, len(leafSets))
	for i, set := range leafSets {
		// create the nmt todo(evan) use nmt wrapper
		tree := nmt.New(sha256.New())
		for _, leaf := range set {
			nsLeaf := append(make([]byte, 0), append(namespace, leaf...)...)
			err := tree.Push(nsLeaf)
			if err != nil {
				return nil, err
			}
		}
		// add the root
		subTreeRoots[i] = tree.Root()
	}
	return merkle.HashFromByteSlices(subTreeRoots), nil
}

// merkleMountainRangeSizes returns the sizes (number of leaf nodes) of the
// trees in a merkle mountain range constructed for a given totalSize and
// maxTreeSize.
//
// https://docs.grin.mw/wiki/chain-state/merkle-mountain-range/
// https://github.com/opentimestamps/opentimestamps-server/blob/master/doc/merkle-mountain-range.md
// TODO: potentially rename function because this doesn't return heights
func merkleMountainRangeSizes(totalSize, maxTreeSize uint64) []uint64 {
	var treeSizes []uint64

	for totalSize != 0 {
		switch {
		case totalSize >= maxTreeSize:
			treeSizes = append(treeSizes, maxTreeSize)
			totalSize = totalSize - maxTreeSize
		case totalSize < maxTreeSize:
			treeSize := appshares.RoundDownPowerOfTwo(totalSize)
			treeSizes = append(treeSizes, treeSize)
			totalSize = totalSize - treeSize
		}
	}

	return treeSizes
}
