package networks

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// archiveRPC serves a /status response describing one node.
func archiveRPC(t *testing.T, chainID, nodeID, listenAddr string, earliest int64, fail bool) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "provider unavailable", http.StatusServiceUnavailable)
			return
		}
		var request struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		result := map[string]any{
			"node_info": map[string]any{
				"id":          nodeID,
				"network":     chainID,
				"listen_addr": listenAddr,
			},
			"sync_info": map[string]any{
				"earliest_block_height": strconv.FormatInt(earliest, 10),
				"latest_block_height":   "100",
				"catching_up":           false,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestFetchArchivePeers(t *testing.T) {
	const (
		archiveID = "daf2cecee2bd7f1b3bf94839f993f807c6b15fbf"
		prunedID  = "53849d640006381d7bdca4d0de9506596c8d59af"
		seedID    = "ee9f90974f85c59d3861fc7f7edb10894f6ac3c8"
	)

	t.Run("keeps archive nodes and drops pruned ones", func(t *testing.T) {
		pruned := archiveRPC(t, "test-chain", prunedID, "1.2.3.4:26656", 1767288, false)
		archive := archiveRPC(t, "test-chain", archiveID, "5.6.7.8:11656", 1, false)
		peers, err := FetchArchivePeers(t.Context(), &Config{ChainID: "test-chain", RPCs: []string{pruned, archive}})
		require.NoError(t, err)
		require.Equal(t, archiveID+"@5.6.7.8:11656", peers)
	})

	t.Run("uses the RPC host when the node advertises a wildcard", func(t *testing.T) {
		// A node advertising tcp://0.0.0.0:26656 says nothing about where it
		// can be reached, so the host the RPC answered on is used instead.
		rpc := archiveRPC(t, "test-chain", archiveID, "tcp://0.0.0.0:26656", 1, false)
		peers, err := FetchArchivePeers(t.Context(), &Config{ChainID: "test-chain", RPCs: []string{rpc}})
		require.NoError(t, err)
		require.Equal(t, archiveID+"@127.0.0.1:26656", peers)
	})

	t.Run("skips seeds", func(t *testing.T) {
		// A seed disconnects right after the address exchange, so it never
		// serves a block even when it keeps the full history.
		seed := archiveRPC(t, "test-chain", seedID, "9.9.9.9:26656", 1, false)
		_, err := FetchArchivePeers(t.Context(), &Config{
			ChainID: "test-chain",
			RPCs:    []string{seed},
			Seeds:   seedID + "@9.9.9.9:26656",
		})
		require.ErrorContains(t, err, "no RPC exposed an archive peer")
	})

	t.Run("rejects a node serving another chain", func(t *testing.T) {
		rpc := archiveRPC(t, "other-chain", archiveID, "5.6.7.8:26656", 1, false)
		_, err := FetchArchivePeers(t.Context(), &Config{ChainID: "test-chain", RPCs: []string{rpc}})
		require.ErrorContains(t, err, "not serving the expected chain")
	})

	t.Run("reports why every RPC was unusable", func(t *testing.T) {
		down := archiveRPC(t, "test-chain", archiveID, "5.6.7.8:26656", 1, true)
		pruned := archiveRPC(t, "test-chain", prunedID, "1.2.3.4:26656", 500000, false)
		_, err := FetchArchivePeers(t.Context(), &Config{ChainID: "test-chain", RPCs: []string{down, pruned}})
		require.ErrorContains(t, err, "no RPC exposed an archive peer")
		require.ErrorContains(t, err, "earliest block height 500000")
	})

	t.Run("requires at least one RPC", func(t *testing.T) {
		_, err := FetchArchivePeers(t.Context(), &Config{ChainID: "test-chain"})
		require.ErrorContains(t, err, "no RPC endpoints configured")
	})
}

func TestMergePeers(t *testing.T) {
	const (
		archiveID = "daf2cecee2bd7f1b3bf94839f993f807c6b15fbf"
		otherID   = "53849d640006381d7bdca4d0de9506596c8d59af"
	)

	t.Run("a discovered address wins over the hardcoded one", func(t *testing.T) {
		merged := MergePeers(archiveID+"@5.6.7.8:26656", archiveID+"@celestia-testnet-peer.itrocket.net:11656")
		require.Equal(t, archiveID+"@5.6.7.8:26656", merged)
	})

	t.Run("keeps the fallback for node IDs that were not discovered", func(t *testing.T) {
		merged := MergePeers(archiveID+"@5.6.7.8:26656", otherID+"@1.2.3.4:26656")
		require.Equal(t, []string{archiveID + "@5.6.7.8:26656", otherID + "@1.2.3.4:26656"}, strings.Split(merged, ","))
	})

	t.Run("ignores empty and malformed entries", func(t *testing.T) {
		require.Empty(t, MergePeers("", "not-a-peer", "@1.2.3.4:26656"))
	})
}
