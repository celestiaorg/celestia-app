package tx_test

import (
	"context"
	"encoding/hex"

	rpcclient "github.com/cometbft/cometbft/rpc/client"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
)

// stubNode is a CometBFT RPC client whose TxStatus and TxStatusBatch return
// one result per hash, in request order. Every other method is left
// unimplemented.
type stubNode struct {
	rpcclient.Client
}

// stubStatus derives a status from the hash so a test can tell which hash a
// returned status belongs to.
func stubStatus(hash []byte) ctypes.ResultTxStatus {
	return ctypes.ResultTxStatus{Height: int64(len(hash)), Status: "S" + hex.EncodeToString(hash), Error: string(hash)}
}

func (s *stubNode) TxStatus(_ context.Context, hash []byte) (*ctypes.ResultTxStatus, error) {
	r := stubStatus(hash)
	return &r, nil
}

func (s *stubNode) TxStatusBatch(_ context.Context, hashes [][]byte) (*ctypes.ResultTxStatusBatch, error) {
	out := make([]ctypes.TxStatusResponse, 0, len(hashes))
	for _, h := range hashes {
		out = append(out, ctypes.TxStatusResponse{Hash: h, Result: stubStatus(h)})
	}
	return &ctypes.ResultTxStatusBatch{Statuses: out}, nil
}
