// txhash queries a block at a given height and prints the correct tx hashes.
// It handles BlobTx unwrapping so that the printed hashes match what the
// tx indexer stores and can be used with the /tx?hash= RPC endpoint.
//
// Usage:
//
//	go run ./scripts/txhash --height 10230356
//	go run ./scripts/txhash --height 10230356 --rpc http://localhost:26657
package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	blobtx "github.com/celestiaorg/go-square/v3/tx"
)

type blockResponse struct {
	Result struct {
		Block struct {
			Data struct {
				Txs [][]byte `json:"txs"` // base64-decoded by json.Unmarshal
			} `json:"data"`
		} `json:"block"`
	} `json:"result"`
}

type txResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    string `json:"data"`
	} `json:"error"`
}

func main() {
	rpc := flag.String("rpc", "http://localhost:26657", "CometBFT RPC endpoint")
	height := flag.Int64("height", 0, "block height to query")
	verify := flag.Bool("verify", true, "verify each hash is queryable via /tx")
	flag.Parse()

	if *height <= 0 {
		fmt.Fprintln(os.Stderr, "error: --height is required and must be positive")
		flag.Usage()
		os.Exit(1)
	}

	// Fetch the block.
	blockURL := fmt.Sprintf("%s/block?height=%d", strings.TrimRight(*rpc, "/"), *height)
	resp, err := http.Get(blockURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error fetching block: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading response: %v\n", err)
		os.Exit(1)
	}

	var block blockResponse
	if err := json.Unmarshal(body, &block); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing block response: %v\n", err)
		os.Exit(1)
	}

	txs := block.Result.Block.Data.Txs
	if len(txs) == 0 {
		fmt.Printf("block %d has no transactions\n", *height)
		return
	}

	fmt.Printf("block %d has %d transaction(s)\n\n", *height, len(txs))

	for i, rawTx := range txs {
		var (
			hashInput []byte
			txType    string
		)

		// Attempt to unmarshal as a BlobTx. If it is one, hash only the
		// inner SDK transaction bytes (without blobs). This matches the
		// behavior of celestia-core's Tx.Hash() method.
		bTx, isBlobTx, err := blobtx.UnmarshalBlobTx(rawTx)
		if isBlobTx && err == nil {
			hashInput = bTx.Tx
			txType = "BlobTx"
		} else {
			hashInput = rawTx
			txType = "Tx"
		}

		hash := sha256.Sum256(hashInput)
		hashHex := fmt.Sprintf("%X", hash[:])

		fmt.Printf("tx[%d] type=%s hash=%s\n", i, txType, hashHex)

		if *verify {
			txURL := fmt.Sprintf("%s/tx?hash=0x%s", strings.TrimRight(*rpc, "/"), hashHex)
			txResp, err := http.Get(txURL)
			if err != nil {
				fmt.Printf("  verification FAILED: %v\n", err)
				continue
			}
			defer txResp.Body.Close()

			txBody, err := io.ReadAll(txResp.Body)
			if err != nil {
				fmt.Printf("  verification FAILED: could not read response: %v\n", err)
				continue
			}

			var result txResponse
			if err := json.Unmarshal(txBody, &result); err != nil {
				fmt.Printf("  verification FAILED: could not parse response: %v\n", err)
				continue
			}

			if result.Error != nil {
				fmt.Printf("  verification FAILED: %s\n", result.Error.Data)
			} else {
				fmt.Printf("  verification OK: tx found in index\n")
			}
		}
	}
}
