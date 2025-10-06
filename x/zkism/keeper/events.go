package keeper

import (
	"encoding/hex"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/celestiaorg/go-square/v2/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EmitCreateISMEvent emits a typed event to signal creation of a new ism.
func EmitCreateISMEvent(ctx sdk.Context, ism types.ZKExecutionISM) error {
	namespace, err := share.NewNamespaceFromBytes(ism.Namespace)
	if err != nil {
		return errorsmod.Wrapf(types.ErrInvalidNamespace, "failed to parse namespace from bytes: %x", ism.Namespace)
	}

	return ctx.EventManager().EmitTypedEvent(&types.EventCreateZKExecutionISM{
		Id:                  ism.Id,
		Owner:               ism.Owner,
		StateRoot:           encodeHex(ism.StateRoot),
		Height:              ism.Height,
		Namespace:           namespace.String(),
		SequencerPublicKey:  encodeHex(ism.SequencerPublicKey),
		Groth16Vkey:         encodeHex(ism.Groth16Vkey),
		StateTransitionVkey: encodeHex(ism.StateTransitionVkey),
		StateMembershipVkey: encodeHex(ism.StateMembershipVkey),
	})
}

// EmitUpdateISMEvent emits a typed event to signal updating of an ism.
func EmitUpdateISMEvent(ctx sdk.Context, ism types.ZKExecutionISM) error {
	return ctx.EventManager().EmitTypedEvent(&types.EventUpdateZKExecutionISM{
		Id:        ism.Id,
		StateRoot: encodeHex(ism.StateRoot),
		Height:    ism.Height,
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

func encodeHex(bz []byte) string {
	return fmt.Sprintf("0x%s", hex.EncodeToString(bz))
}
