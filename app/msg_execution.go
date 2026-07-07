package app

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// executeTxMsgs runs an ante-validated tx's messages on a branch of ctx,
// committing the writes only if all succeed. The tx's ante effects (fee,
// nonce) are untouched either way.
//
// Proposal handlers call this so a later blob tx is checked against the balance
// an earlier MsgSend already spent. Without it the blob tx passes ante-only
// validation but fails fee deduction in FinalizeBlock, committing blobs unpaid.
//
// A nil router skips execution; only tests pass nil.
func executeTxMsgs(ctx sdk.Context, sdkTx sdk.Tx, router baseapp.MessageRouter) (err error) {
	if router == nil {
		return nil
	}
	// Message handlers panic on out of gas; fail the tx instead of crashing.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered panic while executing messages: %v", r)
		}
	}()
	cacheCtx, writeCache := ctx.CacheContext()
	cacheCtx = cacheCtx.WithEventManager(sdk.NewEventManager())
	for _, msg := range sdkTx.GetMsgs() {
		handler := router.Handler(msg)
		if handler == nil {
			return fmt.Errorf("no message handler found for %s", sdk.MsgTypeURL(msg))
		}
		if _, err := handler(cacheCtx, msg); err != nil {
			return err
		}
	}
	writeCache()
	return nil
}
