package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	blobtypes "github.com/celestiaorg/celestia-app/v10/x/blob/types"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	tmtypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestFibreThroughputSuccessfulOnly(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	block := &tmtypes.Block{Header: tmtypes.Header{Height: 10}}
	for i, size := range []uint32{100, 200, 400} {
		msgs := []sdk.Msg{&fibretypes.MsgPayForFibre{
			PaymentPromise: fibretypes.PaymentPromise{BlobSize: size},
		}}
		if i == 1 {
			msgs = append(msgs, &blobtypes.MsgPayForBlobs{BlobSizes: []uint32{10, 20}})
		}
		builder := encCfg.TxConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(msgs...))
		raw, err := encCfg.TxConfig.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		block.Txs = append(block.Txs, raw)
	}
	results := []*abci.ExecTxResult{{Code: 0, GasUsed: 10}, {Code: 5, GasUsed: 20}, {Code: 0, GasUsed: 30}}
	tests := []struct {
		name           string
		successfulOnly bool
		withGas        bool
		results        []*abci.ExecTxResult
		wantCount      int
		wantBytes      int64
		wantIncluded   int64
		wantErr        string
	}{
		{name: "default", results: results, wantCount: 3, wantBytes: 700, wantIncluded: 500},
		{name: "default with gas", withGas: true, results: results, wantCount: 3, wantBytes: 700, wantIncluded: 500},
		{name: "successful only", successfulOnly: true, results: results, wantCount: 2, wantBytes: 500, wantIncluded: 500},
		{name: "successful only with gas", successfulOnly: true, withGas: true, results: results, wantCount: 2, wantBytes: 500, wantIncluded: 500},
		{name: "all failed", results: []*abci.ExecTxResult{{Code: 1}, {Code: 2}, {Code: 3}}, wantCount: 3, wantBytes: 700},
		{name: "missing results", successfulOnly: true, wantErr: "missing execution result for PFF at height 10, tx index 0"},
		{name: "short results", successfulOnly: true, results: results[:1], wantErr: "missing execution result for PFF at height 10, tx index 1"},
		{name: "nil result", successfulOnly: true, withGas: true, results: []*abci.ExecTxResult{results[0], nil, results[2]}, wantErr: "missing execution result for PFF at height 10, tx index 1"},
		{name: "default without results", wantErr: "missing execution result for PFF at height 10, tx index 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resultCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request rpctypes.RPCRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				var result any
				switch request.Method {
				case "block":
					result = &ctypes.ResultBlock{Block: block}
				case "block_results":
					resultCalls.Add(1)
					result = &ctypes.ResultBlockResults{Height: 10, TxsResults: tt.results}
				default:
					t.Errorf("unexpected RPC method: %s", request.Method)
					return
				}
				if err := json.NewEncoder(w).Encode(rpctypes.NewRPCSuccessResponse(request.ID, result)); err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(server.Close)
			client, err := rpchttp.New(server.URL, "/websocket")
			require.NoError(t, err)
			blocks := fetchBlocksConcurrent(t.Context(), []*rpchttp.HTTP{client}, 10, 10, 1, 0, encCfg.TxConfig.TxDecoder(), tt.withGas, tt.successfulOnly)
			require.Len(t, blocks, 1)
			res := blocks[0]
			require.EqualValues(t, 1, resultCalls.Load())
			if tt.wantErr != "" {
				require.EqualError(t, res.err, tt.wantErr)
				return
			}
			require.NoError(t, res.err)
			require.Zero(t, res.decodeErrs)
			require.Equal(t, tt.wantCount, res.pffCount)
			require.Equal(t, tt.wantBytes, res.pffBytes)
			require.Equal(t, tt.wantIncluded, res.pffIncludedBytes)
			require.Equal(t, 1, res.pfbCount)
			require.EqualValues(t, 30, res.pfbBytes)
			if tt.withGas {
				require.Len(t, res.pffGas, 3)
				require.Equal(t, pffGasTrace{Height: 10, TxIndex: 1, BlobSize: 200, Code: 5, GasUsed: 20}, res.pffGas[1])
			} else {
				require.Empty(t, res.pffGas)
			}
		})
	}
}
