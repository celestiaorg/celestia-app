package appd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTelemetryDisableEnv(t *testing.T) {
	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "v3 binary",
			path: "/tmp/bin/celestia-appd-v3",
			want: []string{"CELESTIA_APPD_V3_TELEMETRY_PROMETHEUS_RETENTION_TIME=0"},
		},
		{
			name: "v6 binary",
			path: "/usr/local/bin/celestia-appd-v6",
			want: []string{"CELESTIA_APPD_V6_TELEMETRY_PROMETHEUS_RETENTION_TIME=0"},
		},
		{
			name: "binary with dots in version",
			path: "/tmp/bin/celestia-appd-v3.10.0",
			want: []string{"CELESTIA_APPD_V3_10_0_TELEMETRY_PROMETHEUS_RETENTION_TIME=0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Appd{path: tt.path}
			got := a.telemetryDisableEnv()
			assert.Equal(t, tt.want, got)
		})
	}
}
