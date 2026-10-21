package networks

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchTrustBlock(t *testing.T) {
	for _, tc := range []struct {
		name       string
		firstChain string
		failMethod string
		allFail    bool
	}{
		{name: "unavailable primary", firstChain: "test-chain", failMethod: "status"},
		{name: "missing primary block", firstChain: "test-chain", failMethod: "block"},
		{name: "wrong primary chain", firstChain: "other-chain"},
		{name: "all unavailable", failMethod: "status", allFail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fallbackCalls atomic.Int32
			primary := trustRPC(t, tc.firstChain, tc.failMethod, nil)
			fallbackFailure := ""
			if tc.allFail {
				fallbackFailure = "status"
			}
			fallback := trustRPC(t, "test-chain", fallbackFailure, &fallbackCalls)
			block, err := FetchTrustBlock(t.Context(), &Config{ChainID: "test-chain", RPCs: []string{primary, fallback}}, 10)
			if tc.allFail {
				require.ErrorContains(t, err, "no RPC supplied a usable trust block")
				require.Nil(t, block)
				return
			}
			require.NoError(t, err)
			require.Equal(t, int64(90), block.Block.Height)
			require.Equal(t, int32(2), fallbackCalls.Load())
		})
	}
}

func trustRPC(t *testing.T, chainID, failMethod string, calls *atomic.Int32) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			calls.Add(1)
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if request.Method == failMethod {
			http.Error(w, "provider unavailable", http.StatusServiceUnavailable)
			return
		}
		var result any
		if request.Method == "status" {
			result = map[string]any{
				"node_info": map[string]any{"network": chainID},
				"sync_info": map[string]any{"latest_block_height": "100", "catching_up": false},
			}
		} else {
			result = map[string]any{
				"block_id": map[string]any{"hash": "ABCD"},
				"block":    map[string]any{"header": map[string]any{"chain_id": chainID, "height": "90"}},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	t.Cleanup(server.Close)
	return server.URL
}
