package main

import (
	"context"
	"fmt"
	"github.com/tendermint/tendermint/rpc/client/http"
	ctypes "github.com/tendermint/tendermint/rpc/core/types"
	"time"
)

const mebibyte = 1024 * 1024

func main() {
	start := int64(2560)
	//end:= int64(3803)
	end := int64(2560 + 500)
	count := float64(end - start + 1)
	rpcAddress := "http://localhost:26657"

	trpc, err := http.New(rpcAddress, "/websocket")
	if err != nil {
		panic(err)
	}
	err = trpc.Start()
	if err != nil {
		panic(err)
	}

	totalBytes := float64(0)
	for i := start; i < end; i++ {
		block, err := trpc.Block(context.Background(), &i)
		if err != nil {
			i--
			fmt.Println(err.Error())
			time.Sleep(10 * time.Second)
			continue
		}
		total := toBytes(block)
		totalBytes += total
		fmt.Printf("%d: %d %f %f\n", i, block.Block.SquareSize, total, totalBytes)
	}

	startHeader, err := trpc.Header(context.Background(), &start)
	if err != nil {
		panic(err)
	}
	endHeader, err := trpc.Header(context.Background(), &end)
	if err != nil {
		panic(err)
	}

	totalTime := endHeader.Header.Time.Sub(startHeader.Header.Time).Seconds()

	throughput := totalBytes / totalTime
	fmt.Printf("throughput %.2f bytes %.2f mebibytes/s\n", throughput, throughput/mebibyte)
	fmt.Printf("block time %.2f\n", totalTime/count)
}

func toBytes(block *ctypes.ResultBlock) float64 {
	total := 0
	for _, tx := range block.Block.Data.Txs {
		total += len(tx)
	}
	return float64(total)
}
