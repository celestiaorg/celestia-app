package keeper

import (
	"github.com/celestiaorg/celestia-app/v10/x/teeism/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EmitCreateISMEvent emits a typed event when a new ISM is created.
func EmitCreateISMEvent(ctx sdk.Context, ism types.InterchainSecurityModule) error {
	digest, err := ism.IdentityDigest()
	if err != nil {
		return err
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventCreateInterchainSecurityModule{
		Id:                ism.Id,
		Owner:             ism.Owner,
		State:             types.EncodeHex(ism.State),
		MerkleTreeAddress: types.EncodeHex(ism.MerkleTreeAddress),
		IdentityDigest:    types.EncodeHex(digest[:]),
	})
}

// EmitSubmitAttestationEvent emits a typed event when an attestation advances an ISM.
func EmitSubmitAttestationEvent(ctx sdk.Context, ism types.InterchainSecurityModule, state *types.IsmState, ids [][32]byte) error {
	messages := make([]string, 0, len(ids))
	for i := range ids {
		messages = append(messages, types.EncodeHex(ids[i][:]))
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventSubmitAttestation{
		Id:         ism.Id,
		State:      types.EncodeHex(ism.State),
		StateRoot:  types.EncodeHex(state.StateRoot[:]),
		Height:     state.Height,
		MessageIds: messages,
	})
}
