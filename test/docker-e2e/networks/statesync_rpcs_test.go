package networks

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSelectStateSyncRPCs(t *testing.T) {
	stateSyncProbeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { stateSyncProbeTimeout = 4 * time.Second })

	healthyA := stateSyncRPC(t, "", 0)
	healthyB := stateSyncRPC(t, "", 0)
	slow := stateSyncRPC(t, "", time.Second)
	noValidators := stateSyncRPC(t, "validators", 0)
	noCommit := stateSyncRPC(t, "commit", 0)

	t.Run("drops slow and failing servers in order", func(t *testing.T) {
		rpcs, err := SelectStateSyncRPCs(t.Context(), []string{slow, healthyA, noValidators, healthyB, noCommit}, 90)
		require.NoError(t, err)
		require.Equal(t, []string{healthyA, healthyB}, rpcs)
	})
	t.Run("needs two servers", func(t *testing.T) {
		rpcs, err := SelectStateSyncRPCs(t.Context(), []string{healthyA, slow, noCommit}, 90)
		require.ErrorContains(t, err, "only 1 of 3 RPCs can serve state sync")
		require.Nil(t, rpcs)
	})
}

// stateSyncRPC serves the commit and validators endpoints. failMethod returns
// an error and delay stalls every response.
func stateSyncRPC(t *testing.T, failMethod string, delay time.Duration) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		if request.Method == failMethod {
			http.Error(w, "provider unavailable", http.StatusServiceUnavailable)
			return
		}
		var result any
		switch request.Method {
		case "commit":
			result = map[string]any{"signed_header": map[string]any{}, "canonical": true}
		case "validators":
			result = map[string]any{"block_height": "90", "validators": []any{}, "count": "0", "total": "0"}
		default:
			http.Error(w, "unexpected method "+request.Method, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	t.Cleanup(server.Close)
	return server.URL
}
