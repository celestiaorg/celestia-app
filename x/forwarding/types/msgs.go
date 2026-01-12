package types

import (
	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
)

const URLMsgExecuteForwarding = "/celestia.forwarding.v1.MsgExecuteForwarding"

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

	if len(destRecipient.Bytes()) != 32 {
		return errors.Wrap(ErrAddressMismatch, "dest_recipient must be 32 bytes")
	}

	return nil
}

func (msg *MsgExecuteForwarding) GetSigners() []sdk.AccAddress {
	signer, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{signer}
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
