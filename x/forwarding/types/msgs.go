package types

import (
	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	URLMsgForward      = "/celestia.forwarding.v1.MsgForward"
	URLMsgUpdateParams = "/celestia.forwarding.v1.MsgUpdateParams"
)

var (
	_ sdk.Msg              = &MsgForward{}
	_ sdk.HasValidateBasic = &MsgForward{}
)

func NewMsgForward(signer, forwardAddr string, destDomain uint32, destRecipient string, maxIgpFee sdk.Coin) *MsgForward {
	return &MsgForward{
		Signer:        signer,
		ForwardAddr:   forwardAddr,
		DestDomain:    destDomain,
		DestRecipient: destRecipient,
		MaxIgpFee:     maxIgpFee,
	}
}

func (msg *MsgForward) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	if _, err := sdk.AccAddressFromBech32(msg.ForwardAddr); err != nil {
		return errors.Wrap(err, "invalid forward address")
	}

	destRecipient, err := util.DecodeHexAddress(msg.DestRecipient)
	if err != nil {
		return errors.Wrap(err, "invalid dest_recipient hex format")
	}

	if len(destRecipient.Bytes()) != RecipientLength {
		return errors.Wrapf(ErrInvalidRecipient, "dest_recipient must be %d bytes, got %d", RecipientLength, len(destRecipient.Bytes()))
	}

	// Validate max_igp_fee: must be valid and non-negative
	if err := msg.MaxIgpFee.Validate(); err != nil {
		return errors.Wrap(err, "invalid max_igp_fee")
	}

	return nil
}

var (
	_ sdk.Msg              = &MsgUpdateParams{}
	_ sdk.HasValidateBasic = &MsgUpdateParams{}
)

func (msg *MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return errors.Wrap(err, "invalid authority address")
	}
	return msg.Params.Validate()
}

func NewSuccessResult(denom string, amount math.Int, messageId string) ForwardingResult {
	return ForwardingResult{
		Denom:     denom,
		Amount:    amount,
		MessageId: messageId,
		Success:   true,
		Error:     "",
	}
}

func NewFailureResult(denom string, amount math.Int, errMsg string) ForwardingResult {
	return ForwardingResult{
		Denom:     denom,
		Amount:    amount,
		MessageId: "",
		Success:   false,
		Error:     errMsg,
	}
}
