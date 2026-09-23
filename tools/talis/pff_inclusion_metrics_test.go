package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	"github.com/cometbft/cometbft/state"
	"github.com/stretchr/testify/require"
	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metrics "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

func TestPFFInclusionAvailable(t *testing.T) {
	for _, tt := range []struct {
		name    string
		discard []bool
		want    bool
	}{
		{name: "results retained", discard: []bool{false, false}, want: true},
		{name: "results discarded", discard: []bool{true}, want: false},
		{name: "mixed validators", discard: []bool{false, true}, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clients := make([]*rpchttp.HTTP, 0, len(tt.discard))
			for _, discard := range tt.discard {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var request rpctypes.RPCRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
						return
					}
					if request.Method != "block_results" {
						t.Errorf("unexpected RPC method: %s", request.Method)
					}
					response := rpctypes.NewRPCSuccessResponse(request.ID, &ctypes.ResultBlockResults{Height: 10})
					if discard {
						response = rpctypes.RPCInternalError(request.ID, state.ErrFinalizeBlockResponsesNotPersisted)
					}
					if err := json.NewEncoder(w).Encode(response); err != nil {
						t.Error(err)
					}
				}))
				t.Cleanup(server.Close)
				client, err := rpchttp.New(server.URL, "/websocket")
				require.NoError(t, err)
				clients = append(clients, client)
			}
			require.Equal(t, tt.want, pffInclusionAvailable(t.Context(), clients))
		})
	}
}

func TestPFFInclusionMetrics(t *testing.T) {
	requests := make(chan *collectormetrics.ExportMetricsServiceRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/metrics" {
			t.Errorf("unexpected export path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var request collectormetrics.ExportMetricsServiceRequest
		if err := proto.Unmarshal(body, &request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- &request
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	t.Cleanup(server.Close)

	counter, shutdown, err := setupPFFInclusionMetrics(t.Context(), server.URL)
	require.NoError(t, err)
	counter.Add(t.Context(), 100)
	counter.Add(t.Context(), 400)
	require.NoError(t, shutdown(t.Context()))

	select {
	case request := <-requests:
		require.Len(t, request.ResourceMetrics, 1)
		require.Len(t, request.ResourceMetrics[0].ScopeMetrics, 1)
		exported := request.ResourceMetrics[0].ScopeMetrics[0].Metrics
		require.Len(t, exported, 1)
		require.Equal(t, "fibre.pff.included.bytes", exported[0].Name)
		require.Equal(t, "By", exported[0].Unit)
		sum := exported[0].GetSum()
		require.NotNil(t, sum)
		require.True(t, sum.IsMonotonic)
		require.Equal(t, metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE, sum.AggregationTemporality)
		require.Len(t, sum.DataPoints, 1)
		require.EqualValues(t, 500, sum.DataPoints[0].GetAsInt())
	default:
		t.Fatal("no throughput metrics exported")
	}
}
