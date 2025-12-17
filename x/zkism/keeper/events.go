package keeper

import (
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EmitCreateISMEvent emits a typed event to signal creation of a new ism.
func EmitCreateISMEvent(ctx sdk.Context, ism types.InterchainSecurityModule) error {
	return ctx.EventManager().EmitTypedEvent(&types.EventCreateInterchainSecurityModule{
		Id:                  ism.Id,
		Owner:               ism.Owner,
		State:               encodeHex(ism.State),
		MerkleTreeAddress:   encodeHex(ism.MerkleTreeAddress),
		Groth16Vkey:         encodeHex(ism.Groth16Vkey),
		StateTransitionVkey: encodeHex(ism.StateTransitionVkey),
		StateMembershipVkey: encodeHex(ism.StateMembershipVkey),
	})
}

// EmitUpdateISMEvent emits a typed event to signal updating of an ism.
func EmitUpdateISMEvent(ctx sdk.Context, ism types.InterchainSecurityModule) error {
	return ctx.EventManager().EmitTypedEvent(&types.EventUpdateInterchainSecurityModule{
		Id:    ism.Id,
		State: encodeHex(ism.State),
	})
}

// EmitSubmitMessagesEvent emits a typed event to signal authorization of new messages.
func EmitSubmitMessagesEvent(ctx sdk.Context, root []byte, messageIds [][32]byte) error {
	messages := make([]string, 0, len(messageIds))
	for _, msg := range messageIds {
		messages = append(messages, encodeHex(msg[:]))
	}

	return ctx.EventManager().EmitTypedEvent(&types.EventSubmitMessages{
		StateRoot: encodeHex(root),
		Messages:  messages,
	})
}
