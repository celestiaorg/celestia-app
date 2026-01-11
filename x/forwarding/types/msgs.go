package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
)

const (
	// URLMsgExecuteForwarding is the type URL for MsgExecuteForwarding
	URLMsgExecuteForwarding = "/celestia.forwarding.v1.MsgExecuteForwarding"
)

var _ sdk.Msg = &MsgExecuteForwarding{}

// NewMsgExecuteForwarding creates a new MsgExecuteForwarding
func NewMsgExecuteForwarding(signer, forwardAddr string, destDomain uint32, destRecipient string) *MsgExecuteForwarding {
	return &MsgExecuteForwarding{
		Signer:        signer,
		ForwardAddr:   forwardAddr,
		DestDomain:    destDomain,
		DestRecipient: destRecipient,
	}
}

// ValidateBasic performs basic validation
func (msg *MsgExecuteForwarding) ValidateBasic() error {
	// Validate signer address
	_, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// Validate forward address
	_, err = sdk.AccAddressFromBech32(msg.ForwardAddr)
	if err != nil {
		return errors.Wrap(err, "invalid forward address")
	}

	// Validate dest_recipient is valid hex and 32 bytes
	destRecipient, err := util.DecodeHexAddress(msg.DestRecipient)
	if err != nil {
		return errors.Wrap(err, "invalid dest_recipient hex format")
	}

	// destRecipient must be exactly 32 bytes
	if len(destRecipient.Bytes()) != 32 {
		return errors.Wrap(ErrAddressMismatch, "dest_recipient must be 32 bytes")
	}

	return nil
}

// GetSigners returns the expected signers for the message
func (msg *MsgExecuteForwarding) GetSigners() []sdk.AccAddress {
	signer, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{signer}
}
