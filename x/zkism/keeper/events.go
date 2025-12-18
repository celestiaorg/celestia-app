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
		State:               types.EncodeHex(ism.State),
		MerkleTreeAddress:   types.EncodeHex(ism.MerkleTreeAddress),
		Groth16Vkey:         types.EncodeHex(ism.Groth16Vkey),
		StateTransitionVkey: types.EncodeHex(ism.StateTransitionVkey),
		StateMembershipVkey: types.EncodeHex(ism.StateMembershipVkey),
	})
}

// EmitUpdateISMEvent emits a typed event to signal updating of an ism.
func EmitUpdateISMEvent(ctx sdk.Context, ism types.InterchainSecurityModule) error {
	return ctx.EventManager().EmitTypedEvent(&types.EventUpdateInterchainSecurityModule{
		Id:    ism.Id,
		State: types.EncodeHex(ism.State),
	})
}

// EmitSubmitMessagesEvent emits a typed event to signal authorization of new messages.
func EmitSubmitMessagesEvent(ctx sdk.Context, root []byte, messageIds [][32]byte) error {
	messages := make([]string, 0, len(messageIds))
	for _, msg := range messageIds {
		messages = append(messages, types.EncodeHex(msg[:]))
	}

	return ctx.EventManager().EmitTypedEvent(&types.EventSubmitMessages{
		StateRoot: types.EncodeHex(root),
		Messages:  messages,
	})
}
