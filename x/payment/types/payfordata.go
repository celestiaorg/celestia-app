package types

import (
	"bytes"
	"fmt"
	"math/bits"

	shares "github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/rsmt2d"
	sdkclient "github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/tendermint/tendermint/crypto/merkle"
	"github.com/tendermint/tendermint/pkg/consts"
	"github.com/tendermint/tendermint/pkg/wrapper"
	coretypes "github.com/tendermint/tendermint/types"
)

const (
	URLMsgWirePayForData = "/payment.MsgWirePayForData"
	URLMsgPayForData     = "/payment.MsgPayForData"
	ShareSize            = consts.ShareSize
	SquareSize           = consts.MaxSquareSize
	NamespaceIDSize      = consts.NamespaceSize
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

	// ensure that ParitySharesNamespaceID is not used
	if bytes.Equal(msg.GetMessageNamespaceId(), consts.ParitySharesNamespaceID) {
		return ErrParitySharesNamespace
	}

	// ensure that TailPaddingNamespaceID is not used
	if bytes.Equal(msg.GetMessageNamespaceId(), consts.TailPaddingNamespaceID) {
		return ErrTailPaddingNamespace
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

// CreateCommitment generates the commit bytes for a given message, namespace, and
// squaresize using a namespace merkle tree and the rules described at
// https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md#message-layout-rationale
func CreateCommitment(k uint64, namespace, message []byte) ([]byte, error) {
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
	shares := shares.SplitMessagesIntoShares(msg).RawShares()
	// if the number of shares is larger than that in the square, throw an error
	// note, we use k*k-1 here because at least a single share will be reserved
	// for the transaction paying for the message, therefore the max number of
	// shares a message can be is number of shares in square -1.
	if uint64(len(shares)) > (k*k)-1 {
		return nil, fmt.Errorf("message size exceeds max shares for square size %d: max %d taken %d", k, (k*k)-1, len(shares))
	}

	// organize shares for merkle mountain range
	heights := powerOf2MountainRange(uint64(len(shares)), k)
	leafSets := make([][][]byte, len(heights))
	cursor := uint64(0)
	for i, height := range heights {
		leafSets[i] = shares[cursor : cursor+height]
		cursor = cursor + height
	}

	// create the commits by pushing each leaf set onto an nmt
	subTreeRoots := make([][]byte, len(leafSets))
	for i, set := range leafSets {
		tree := wrapper.NewErasuredNamespacedMerkleTree(consts.MaxSquareSize)
		for _, leaf := range set {
			nsLeaf := append(make([]byte, 0), append(namespace, leaf...)...)
			// here we hardcode pushing as axis 0 cell 0 because we never want
			// to add the parity namespace to our shares when we create roots.
			tree.Push(nsLeaf, rsmt2d.SquareIndex{Axis: 0, Cell: 0})
		}
		subTreeRoots[i] = tree.Root()
	}
	return merkle.HashFromByteSlices(subTreeRoots), nil
}

// powerOf2MountainRange returns the heights of the subtrees for binary merkle
// mountain range
func powerOf2MountainRange(l, k uint64) []uint64 {
	var output []uint64

	for l != 0 {
		switch {
		case l >= k:
			output = append(output, k)
			l = l - k
		case l < k:
			p := nextLowestPowerOf2(l)
			output = append(output, p)
			l = l - p
		}
	}

	return output
}

// NextHighestPowerOf2 returns the next highest power of 2.
// Examples:
// NextHighestPowerOf2(1) = 2
// NextHighestPowerOf2(2) = 4
// NextHighestPowerOf2(5) = 8
func NextHighestPowerOf2(v uint64) uint64 {
	// keep track of the value to check if its the same later
	i := v

	// find the next highest power using bit mashing
	v--
	v |= v >> 1
	v |= v >> 2
	v |= v >> 4
	v |= v >> 8
	v |= v >> 16
	v |= v >> 32
	v++

	// force the value to the next highest power of two if its the same
	if v == i {
		return 2 * v
	}

	return v
}

func nextLowestPowerOf2(v uint64) uint64 {
	c := NextHighestPowerOf2(v)
	if c == v {
		return c
	}
	return c / 2
}

// Check if number is power of 2
func powerOf2(v uint64) bool {
	if v&(v-1) == 0 && v != 0 {
		return true
	}
	return false
}

// DelimLen calculates the length of the delimiter for a given message size
func DelimLen(x uint64) int {
	return 8 - bits.LeadingZeros64(x)%8
}
