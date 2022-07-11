package main

import (
	"context"
	"fmt"

	"github.com/tendermint/tendermint/rpc/client/http"
)

// TODO delete this file
func main() {
	ctx := context.Background()
	trpc, err := http.New("http://localhost:26657", "/websocket")
	if err != nil {
		return
	}
	err = trpc.Start()
	if err != nil {
		return
	}

	//nolint
	results, err := trpc.Subscribe(ctx, "valset-changes", "eventType.eventAttribute='valset_request'")
	// trpc.
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-results:
			fmt.Printf("Got Something")
			attributes := ev.Events
			fmt.Println(attributes)
		}
	}
}
