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
	URLMsgWirePayForData = "/payment.MsgWirePayForData"
	URLMsgPayForData     = "/payment.MsgPayForData"
	ShareSize            = appconsts.ShareSize
	SquareSize           = appconsts.MaxSquareSize
	NamespaceIDSize      = appconsts.NamespaceSize
)

var _ sdk.Msg = &MsgPayForData{}

// Route fullfills the sdk.Msg interface
func (msg *MsgPayForData) Route() string { return RouterKey }

// Type fullfills the sdk.Msg interface
func (msg *MsgPayForData) Type() string {
	return URLMsgPayForData
}

// ValidateBasic fullfills the sdk.Msg interface by performing stateless
// validity checks on the msg that also don't require having the actual message
func (msg *MsgPayForData) ValidateBasic() error {
	if err := ValidateMessageNamespaceID(msg.GetMessageNamespaceId()); err != nil {
		return err
	}

	_, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return err
	}

	if len(msg.MessageShareCommitment) == 0 {
		return ErrNoMessageShareCommitments
	}

	return nil
}

// GetSignBytes fullfills the sdk.Msg interface by reterning a deterministic set
// of bytes to sign over
func (msg *MsgPayForData) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

// GetSigners fullfills the sdk.Msg interface by returning the signer's address
func (msg *MsgPayForData) GetSigners() []sdk.AccAddress {
	address, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{address}
}

// BuildPayForDataTxFromWireTx creates an authsigning.Tx using data from the original
// MsgWirePayForData sdk.Tx and the signature provided. This is used while processing
// the MsgWirePayForDatas into Signed  MsgPayForData
func BuildPayForDataTxFromWireTx(
	origTx authsigning.Tx,
	builder sdkclient.TxBuilder,
	signature []byte,
	msg *MsgPayForData,
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
		return nil, fmt.Errorf("unexpected number of signers: %d", len(origSigs))
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

// CreateCommitment generates the commit bytes for a given squareSize,
// namespace, and message using a namespace merkle tree and the rules described
// at
// https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md#message-layout-rationale
func CreateCommitment(squareSize uint64, namespace, message []byte) ([]byte, error) {
	msg := coretypes.Messages{
		MessagesList: []coretypes.Message{
			{
				NamespaceID: namespace,
				Data:        message,
			},
		},
	}

	// split into shares that are length delimited and include the namespace in
	// each share
	shares, err := appshares.SplitMessages(0, nil, msg.MessagesList, false)
	if err != nil {
		return nil, err
	}
	// if the number of shares is larger than that in the square, throw an error
	// note, we use (squareSize*squareSize)-1 here because at least a single
	// share will be reserved for the transaction paying for the message,
	// therefore the max number of shares a message can be is number of shares
	// in square - 1.
	if uint64(len(shares)) > (squareSize*squareSize)-1 {
		return nil, fmt.Errorf("message size exceeds max shares for square size %d: max %d taken %d", squareSize, (squareSize*squareSize)-1, len(shares))
	}

	// organize shares for merkle mountain range
	heights := powerOf2MountainRange(uint64(len(shares)), squareSize)
	leafSets := make([][][]byte, len(heights))
	cursor := uint64(0)
	for i, height := range heights {
		leafSets[i] = appshares.ToBytes(shares[cursor : cursor+height])
		cursor = cursor + height
	}

	// create the commits by pushing each leaf set onto an nmt
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

// powerOf2MountainRange returns the heights of the subtrees for binary merkle
// mountain range
func powerOf2MountainRange(l, squareSize uint64) []uint64 {
	var output []uint64

	for l != 0 {
		switch {
		case l >= squareSize:
			output = append(output, squareSize)
			l = l - squareSize
		case l < squareSize:
			p := appshares.RoundDownPowerOfTwo(l)
			output = append(output, p)
			l = l - p
		}
	}

	return output
}
