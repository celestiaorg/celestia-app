package keeper

import (
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
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

// EmitSubmitMessagesEvent emits a typed event to signal a newly proven source chain merkle tree state.
func EmitSubmitMessagesEvent(ctx sdk.Context, ism types.InterchainSecurityModule, treeState types.MerkleTreeState) error {
	return ctx.EventManager().EmitTypedEvent(&types.EventSubmitMessages{
		Id:        ism.Id,
		StateRoot: types.EncodeHex(ism.State[:32]),
		TreeRoot:  types.EncodeHex(treeState.Root),
		Count:     treeState.Count,
	})
}

// EmitUpdateVerifyingKeysEvent emits a typed event to signal replacement of the verifying keys of an ism.
func EmitUpdateVerifyingKeysEvent(ctx sdk.Context, ism types.InterchainSecurityModule) error {
	return ctx.EventManager().EmitTypedEvent(&types.EventUpdateVerifyingKeys{
		Id:                  ism.Id,
		Groth16Vkey:         types.EncodeHex(ism.Groth16Vkey),
		StateTransitionVkey: types.EncodeHex(ism.StateTransitionVkey),
		StateMembershipVkey: types.EncodeHex(ism.StateMembershipVkey),
	})
}

// EmitTransferOwnershipEvent emits a typed event to signal that ownership of an ism was transferred or renounced.
func EmitTransferOwnershipEvent(ctx sdk.Context, ismId util.HexAddress, msg *types.MsgTransferOwnership) error {
	return ctx.EventManager().EmitTypedEvent(&types.EventTransferOwnership{
		Id:                ismId,
		Owner:             msg.Owner,
		NewOwner:          msg.NewOwner,
		RenounceOwnership: msg.RenounceOwnership,
	})
}
