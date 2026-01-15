package types

import (
	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
)

const (
	URLMsgExecuteForwarding      = "/celestia.forwarding.v1.MsgExecuteForwarding"
	URLMsgUpdateForwardingParams = "/celestia.forwarding.v1.MsgUpdateForwardingParams"
)

var _ sdk.Msg = &MsgExecuteForwarding{}

func NewMsgExecuteForwarding(signer, forwardAddr string, destDomain uint32, destRecipient string) *MsgExecuteForwarding {
	return &MsgExecuteForwarding{
		Signer:        signer,
		ForwardAddr:   forwardAddr,
		DestDomain:    destDomain,
		DestRecipient: destRecipient,
	}
}

func (msg *MsgExecuteForwarding) ValidateBasic() error {
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
		return errors.Wrap(ErrAddressMismatch, "dest_recipient must be 32 bytes")
	}

	return nil
}

var _ sdk.Msg = &MsgUpdateForwardingParams{}

func (msg *MsgUpdateForwardingParams) ValidateBasic() error {
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
