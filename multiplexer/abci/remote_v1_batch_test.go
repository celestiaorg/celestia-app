package abci

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"testing"

	abciv2 "github.com/cometbft/cometbft/abci/types"
	typesv2 "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"
	abciv1 "github.com/tendermint/tendermint/abci/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func setMuxBatch(t *testing.T, on bool) {
	t.Helper()
	prev := muxBatchEnabled
	muxBatchEnabled = on
	t.Cleanup(func() { muxBatchEnabled = prev })
}

// batchChild simulates the v3 child: execTx is its DeliverTx, and with
// supportsBatch it serves batches the way the patched celestia-core
// GRPCApplication does (only when the call carries the batch metadata).
type batchChild struct {
	mockABCIApplicationClient
	supportsBatch bool
	// corrupt, if set, rewrites a batch response's Data.
	corrupt func([]byte) []byte
	// failAfter, if > 0, makes the RPC fail once that many txs have run.
	failAfter int

	executed [][]byte
	rpcs     int
}

func execTx(tx []byte) abciv1.ResponseDeliverTx {
	if len(tx) == 0 {
		return abciv1.ResponseDeliverTx{Code: 2, Log: "empty tx", Codespace: "sdk"}
	}
	return abciv1.ResponseDeliverTx{
		Code:      uint32(tx[0] % 3),
		Data:      tx[:len(tx)/2],
		Log:       fmt.Sprintf("log %x", tx[0]),
		Info:      "info",
		GasWanted: int64(len(tx) * 2),
		GasUsed:   int64(len(tx)),
		Events: []abciv1.Event{{Type: "tx", Attributes: []abciv1.EventAttribute{
			{Key: "k", Value: string(tx), Index: tx[0]%2 == 0},
		}}},
		Codespace: "sdk",
	}
}

// wire simulates the gRPC proto round trip of a response.
func wire(t *testing.T, r abciv1.ResponseDeliverTx) *abciv1.ResponseDeliverTx {
	bz, err := r.Marshal()
	require.NoError(t, err)
	out := new(abciv1.ResponseDeliverTx)
	require.NoError(t, out.Unmarshal(bz))
	return out
}

func (c *batchChild) run(tx []byte) error {
	if c.failAfter > 0 && len(c.executed) >= c.failAfter {
		return errors.New("child died")
	}
	c.executed = append(c.executed, tx)
	return nil
}

func (c *batchChild) deliverTx(t *testing.T) func(ctx context.Context, req *abciv1.RequestDeliverTx, _ ...grpc.CallOption) (*abciv1.ResponseDeliverTx, error) {
	return func(ctx context.Context, req *abciv1.RequestDeliverTx, _ ...grpc.CallOption) (*abciv1.ResponseDeliverTx, error) {
		c.rpcs++
		md, _ := metadata.FromOutgoingContext(ctx)
		v := md.Get(deliverTxBatchMetadataKey)
		if c.supportsBatch && len(v) == 1 && v[0] == deliverTxBatchMetadataValue && bytes.HasPrefix(req.Tx, []byte(deliverTxBatchMagic)) {
			b := req.Tx[len(deliverTxBatchMagic):]
			var data []byte
			var tmp [binary.MaxVarintLen64]byte
			for len(b) > 0 {
				l, k := binary.Uvarint(b)
				require.Positive(t, k)
				tx := b[k : k+int(l)]
				b = b[k+int(l):]
				if err := c.run(tx); err != nil {
					return nil, err
				}
				r := execTx(tx)
				bz, err := r.Marshal()
				require.NoError(t, err)
				data = append(data, tmp[:binary.PutUvarint(tmp[:], uint64(len(bz)))]...)
				data = append(data, bz...)
			}
			if c.corrupt != nil {
				data = c.corrupt(data)
			}
			return wire(t, abciv1.ResponseDeliverTx{Data: data, Info: deliverTxBatchMagic}), nil
		}
		if bytes.HasPrefix(req.Tx, []byte(deliverTxBatchMagic)) && len(v) == 1 {
			// An unpatched child fails to decode the batch payload; it
			// does not change state, so it is not recorded as executed.
			return wire(t, abciv1.ResponseDeliverTx{Code: 2, Log: "tx parse error", Codespace: "sdk"}), nil
		}
		if err := c.run(req.Tx); err != nil {
			return nil, err
		}
		return wire(t, execTx(req.Tx)), nil
	}
}

type batchChildClient struct {
	*batchChild
	deliver func(ctx context.Context, req *abciv1.RequestDeliverTx, opts ...grpc.CallOption) (*abciv1.ResponseDeliverTx, error)
}

func (c batchChildClient) DeliverTx(ctx context.Context, req *abciv1.RequestDeliverTx, opts ...grpc.CallOption) (*abciv1.ResponseDeliverTx, error) {
	return c.deliver(ctx, req, opts...)
}

func newBatchChild(t *testing.T, supportsBatch bool) (*batchChild, *RemoteABCIClientV1) {
	c := &batchChild{
		mockABCIApplicationClient: mockABCIApplicationClient{
			infoResp:       &abciv1.ResponseInfo{AppVersion: 1},
			beginBlockResp: &abciv1.ResponseBeginBlock{Events: []abciv1.Event{{Type: "begin"}}},
			endBlockResp:   &abciv1.ResponseEndBlock{Events: []abciv1.Event{{Type: "end"}}},
			commitResp:     &abciv1.ResponseCommit{Data: []byte("apphash")},
		},
		supportsBatch: supportsBatch,
	}
	return c, &RemoteABCIClientV1{ABCIApplicationClient: batchChildClient{c, c.deliverTx(t)}}
}

func finalizeReq(txs [][]byte) *abciv2.RequestFinalizeBlock {
	return &abciv2.RequestFinalizeBlock{Height: 1, Header: &typesv2.Header{}, Txs: txs}
}

// randBlocks returns blocks with 0..20 random txs, including empty txs and raw
// txs that start with the batch magic (app v1 allowed undecodable txs).
func randBlocks(r *rand.Rand, n int) [][][]byte {
	blocks := make([][][]byte, n)
	for i := range blocks {
		txs := make([][]byte, r.Intn(21))
		for j := range txs {
			switch r.Intn(6) {
			case 0:
				txs[j] = []byte{}
			case 1:
				// A raw tx that is itself a well-formed batch payload.
				txs[j] = append([]byte(deliverTxBatchMagic), 3, 0x0a, 1, 2)
			default:
				txs[j] = make([]byte, 1+r.Intn(300))
				r.Read(txs[j])
			}
		}
		blocks[i] = txs
	}
	// Always include a single magic-prefixed tx block.
	blocks = append(blocks, [][]byte{append([]byte(deliverTxBatchMagic), 3, 0x0a, 1, 2)})
	return blocks
}

func finalizeAll(t *testing.T, client *RemoteABCIClientV1, blocks [][][]byte) []*abciv2.ResponseFinalizeBlock {
	out := make([]*abciv2.ResponseFinalizeBlock, len(blocks))
	for i, txs := range blocks {
		resp, err := client.FinalizeBlock(finalizeReq(txs))
		require.NoError(t, err)
		out[i] = resp
	}
	return out
}

// TestDeliverTxBatchMatchesPerTx checks that FinalizeBlock returns identical
// responses (codes, data, gas, logs, events, app hash) and the child executes
// the same txs in the same order with batching off, on with a batch-capable
// child, and on with an unpatched child (fallback).
func TestDeliverTxBatchMatchesPerTx(t *testing.T) {
	blocks := randBlocks(rand.New(rand.NewSource(7)), 40)
	var allTxs [][]byte
	for _, b := range blocks {
		allTxs = append(allTxs, b...)
	}

	setMuxBatch(t, false)
	stockChild, stockClient := newBatchChild(t, true)
	want := finalizeAll(t, stockClient, blocks)
	require.Equal(t, allTxs, stockChild.executed)
	require.Equal(t, len(allTxs), stockChild.rpcs)

	setMuxBatch(t, true)
	batchChild, batchClient := newBatchChild(t, true)
	require.Equal(t, want, finalizeAll(t, batchClient, blocks))
	require.Equal(t, allTxs, batchChild.executed)
	require.Less(t, batchChild.rpcs, stockChild.rpcs)
	require.False(t, batchClient.batchUnsupported)

	oldChild, oldClient := newBatchChild(t, false)
	require.Equal(t, want, finalizeAll(t, oldClient, blocks))
	require.Equal(t, allTxs, oldChild.executed)
	require.True(t, oldClient.batchUnsupported)
	// Only one batch attempt is made against an unpatched child.
	require.Equal(t, len(allTxs)+1, oldChild.rpcs)
}

func TestDeliverTxBatchErrors(t *testing.T) {
	setMuxBatch(t, true)
	txs := [][]byte{{1, 2}, {3, 4}, {5, 6}}

	for name, corrupt := range map[string]func([]byte) []byte{
		"truncated":     func(b []byte) []byte { return b[:len(b)-1] },
		"missing resp":  func(b []byte) []byte { return b[:1+int(b[0])] },
		"extra resp":    func(b []byte) []byte { return append(b, b[:1+int(b[0])]...) },
		"bad length":    func(b []byte) []byte { return append(b, 0xff) },
		"bad proto":     func(b []byte) []byte { return append(b, 1, 0xff) },
		"empty payload": func([]byte) []byte { return nil },
	} {
		child, client := newBatchChild(t, true)
		child.corrupt = corrupt
		_, err := client.FinalizeBlock(finalizeReq(txs))
		require.Error(t, err, name)
		// Must not fall back and re-execute txs the child already ran.
		require.Equal(t, 1, child.rpcs, name)
	}

	// The child dying mid-batch is an error; txs are not re-sent individually.
	child, client := newBatchChild(t, true)
	child.failAfter = 2
	_, err := client.FinalizeBlock(finalizeReq(txs))
	require.Error(t, err)
	require.Equal(t, 1, child.rpcs)
}
