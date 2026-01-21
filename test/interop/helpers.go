package interop

import (
	"encoding/hex"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
)

// ExtractDispatchMessage extracts the Hyperlane message from dispatch events.
// Returns empty string if no dispatch event is found.
func ExtractDispatchMessage(events []abci.Event) string {
	for _, evt := range events {
		if evt.Type == proto.MessageName(&coretypes.EventDispatch{}) {
			protoMsg, err := sdk.ParseTypedEvent(evt)
			if err != nil {
				continue
			}
			if eventDispatch, ok := protoMsg.(*coretypes.EventDispatch); ok {
				return eventDispatch.Message
			}
		}
	}
	return ""
}

// CountDispatchEvents counts the number of EventDispatch occurrences in events.
func CountDispatchEvents(events []abci.Event) int {
	count := 0
	for _, evt := range events {
		if evt.Type == proto.MessageName(&coretypes.EventDispatch{}) {
			count++
		}
	}
	return count
}

// MakeRecipient32 pads a 20-byte address to a 32-byte Hyperlane recipient.
// The address bytes are placed at the end (bytes 12-31).
func MakeRecipient32(addr sdk.AccAddress) []byte {
	recipient := make([]byte, 32)
	copy(recipient[12:], addr.Bytes())
	return recipient
}

// RecipientToHex converts a 32-byte recipient to a HexAddress.
func RecipientToHex(recipient []byte) util.HexAddress {
	hexAddr, _ := util.DecodeHexAddress("0x" + hex.EncodeToString(recipient))
	return hexAddr
}
