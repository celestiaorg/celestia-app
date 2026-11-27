package testnode

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v12/internal/version"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

func TestValidatorVersionReporting(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a consensus node")
	}
	cctx, _, _ := NewNetwork(t, DefaultConfig())
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	reports := make(chan *collectormetrics.ExportMetricsServiceRequest, 1)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var report collectormetrics.ExportMetricsServiceRequest
		if err := proto.Unmarshal(body, &report); err != nil {
			t.Error(err)
			return
		}
		select {
		case reports <- &report:
		default:
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer collector.Close()
	v := viper.New()
	v.Set("version-reporting.endpoint", collector.URL)
	v.Set("version-reporting.interval", "20ms")
	done := make(chan struct{})
	go func() {
		defer close(done)
		version.Run(ctx, cctx.tmNode, v, log.NewNopLogger())
	}()
	defer func() { cancel(); <-done }()
	select {
	case report := <-reports:
		pubKey, err := cctx.tmNode.PrivValidator().GetPubKey()
		require.NoError(t, err)
		require.Len(t, report.ResourceMetrics, 1)
		point := report.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetGauge().DataPoints[0]
		attrs := map[string]string{}
		for _, a := range point.Attributes {
			attrs[a.Key] = a.Value.GetStringValue()
		}
		require.Equal(t, pubKey.Address().String(), attrs["consensus_address"])
		require.Equal(t, cctx.tmNode.GenesisDoc().ChainID, attrs["chain_id"])
	case <-ctx.Done():
		t.Fatal("active validator did not report its version")
	}
}
