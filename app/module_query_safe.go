package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
)

// maxModuleQuerySafeRequests is the most requests a MsgModuleQuerySafe may
// carry for this node to admit it to the mempool or include it in a proposal.
//
// Each request counts as one message against the per-block MaxSDKMessages
// budget but costs only a few bytes, so without a bound one small tx could take
// the whole budget and keep every other SDK tx out of the block.
//
// This is a local mempool and proposal policy, not a validity rule:
// ProcessProposal and FinalizeBlock still accept such a tx, so blocks built by
// nodes without this check stay valid.
const maxModuleQuerySafeRequests = 1

// validateModuleQuerySafeRequests returns an error if tx carries a
// MsgModuleQuerySafe with more than maxModuleQuerySafeRequests requests, either
// at the top level or one level down in a MsgExec or MsgSubmitProposal. Deeper
// nesting is not inspected because the ante handler rejects it.
func validateModuleQuerySafeRequests(tx sdk.Tx) error {
	for _, msg := range tx.GetMsgs() {
		if err := checkModuleQuerySafeRequests(msg); err != nil {
			return err
		}
		var (
			nested []sdk.Msg
			err    error
		)
		switch m := msg.(type) {
		case *authz.MsgExec:
			nested, err = m.GetMessages()
		case *govv1.MsgSubmitProposal:
			nested, err = m.GetMsgs()
		}
		if err != nil {
			// Undecodable inner messages are rejected by the ante handler.
			// Leave that to it rather than changing which error is returned.
			continue
		}
		for _, nestedMsg := range nested {
			if err := checkModuleQuerySafeRequests(nestedMsg); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkModuleQuerySafeRequests(msg sdk.Msg) error {
	m, ok := msg.(*icahosttypes.MsgModuleQuerySafe)
	if !ok {
		return nil
	}
	if len(m.Requests) > maxModuleQuerySafeRequests {
		return sdkerrors.ErrInvalidRequest.Wrapf("MsgModuleQuerySafe has %d requests, limit is %d", len(m.Requests), maxModuleQuerySafeRequests)
	}
	return nil
}
