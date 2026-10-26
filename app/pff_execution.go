package app

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// executeTxMsgs executes a transaction's messages on a cached branch of ctx,
// committing writes only if every message succeeds.
func executeTxMsgs(ctx sdk.Context, sdkTx sdk.Tx, router baseapp.MessageRouter) error {
	// Discard all message writes if any handler fails.
	cacheCtx, writeCache := ctx.CacheContext()
	if err := runMsgs(cacheCtx, sdkTx.GetMsgs(), router); err != nil {
		return err
	}
	writeCache()
	return nil
}

// runMsgs executes msgs directly on ctx, so a caller that must not keep a failure's
// partial writes branches first. A nil router skips execution for tests.
func runMsgs(ctx sdk.Context, msgs []sdk.Msg, router baseapp.MessageRouter) (err error) {
	if router == nil {
		return nil
	}
	// Message handlers panic on out of gas; fail the tx instead of crashing.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered panic while executing messages: %v", r)
		}
	}()
	for _, msg := range msgs {
		handler := router.Handler(msg)
		if handler == nil {
			return fmt.Errorf("no message handler found for %s", sdk.MsgTypeURL(msg))
		}
		if _, err := handler(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}
