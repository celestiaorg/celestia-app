package app

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// executeTxMsgs executes a transaction's messages on a cached branch of ctx,
// committing writes only if every message succeeds.
// A nil router skips execution for tests.
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
	// Discard all message writes if any handler fails.
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
