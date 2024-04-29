package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"

	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/tendermint/tendermint/rpc/client/http"
	"github.com/tendermint/tendermint/types"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Println("ERR:", err)
	}
}

func Run(ctx context.Context) error {
	if len(os.Args) < 2 || len(os.Args) > 4 {
		fmt.Print(`
Usage: blockscan <rpc-address> [from-height] [to-height]
`)
		return nil
	}
	var (
		err                  error
		fromHeight, toHeight int64
	)

	if len(os.Args) >= 3 {
		fromHeight, err = strconv.ParseInt(os.Args[2], 10, 64)
		if err != nil {
			return err
		}
	}
	if len(os.Args) == 4 {
		toHeight, err = strconv.ParseInt(os.Args[3], 10, 64)
		if err != nil {
			return err
		}
	}

	return Scan(ctx, os.Args[1], fromHeight, toHeight)
}

func Scan(ctx context.Context, rpcAddress string, fromHeight, toHeight int64) error {
	client, err := http.New(rpcAddress, "/websocket")
	if err != nil {
		return err
	}

	status, err := client.Status(ctx)
	if err != nil {
		return err
	}

	if fromHeight == 0 && toHeight == 0 {
		fmt.Printf("Trailing chain %s...\n", status.NodeInfo.Network)
		return Trail(ctx, client)
	}

	if toHeight == 0 {
		toHeight = fromHeight
		fmt.Printf("Scanning height %d...\n", fromHeight)
	} else {
		if fromHeight > toHeight {
			return fmt.Errorf("fromHeight must be less or equal to toHeight")
		}
		fmt.Printf("Scanning from height %d to %d...\n", fromHeight, toHeight)
	}

	for height := fromHeight; height <= toHeight; height++ {
		block, err := client.Block(ctx, &height)
		if err != nil {
			return err
		}
		if err := PrintBlock(block.Block); err != nil {
			return err
		}
	}

	return nil
}

func Trail(ctx context.Context, client *http.HTTP) error {
	if err := client.Start(); err != nil {
		return err
	}
	defer func() {
		if err := client.Stop(); err != nil {
			log.Println("ERR:", err)
		}
	}()
	sub, err := client.Subscribe(ctx, "blockscan", "tm.event='NewBlock'")
	if err != nil {
		return err
	}
	defer func() {
		if err := client.UnsubscribeAll(ctx, "blockscan"); err != nil {
			log.Println("ERR:", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case result := <-sub:
			blockResult, ok := result.Data.(types.EventDataNewBlock)
			if !ok {
				return fmt.Errorf("unexpected result type: %T", result.Data)
			}
			if err := PrintBlock(blockResult.Block); err != nil {
				return err
			}
		}
	}
}

func PrintBlock(block *types.Block) error {
	fmt.Println("Height:", block.Height)
	txs, err := testnode.DecodeBlockData(block.Data)
	if err != nil {
		return err
	}
	for _, tx := range txs {
		authTx, ok := tx.(authsigning.Tx)
		if !ok {
			return fmt.Errorf("tx is not an auth.Tx")
		}
		PrintTx(authTx)
	}
	return nil
}

func PrintTx(tx authsigning.Tx) {
	msgs := tx.GetMsgs()
	fmt.Printf(`Tx - Signer: %s, Fee: %s {
%s
}
`, tx.GetSigners(), tx.GetFee(), printMessages(msgs))
}

func printMessages(msgs []sdk.Msg) string {
	output := ""
	for _, msg := range msgs {
		output += fmt.Sprintf("  - %s\n", sdk.MsgTypeURL(msg))
	}
	return output
}
