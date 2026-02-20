package appd

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		setenv map[string]string // env vars to set before calling getEnv
		assert func(t *testing.T, a *Appd, env []string)
	}{
		{
			name: "includes all os.Environ and telemetry disable var",
			path: "/tmp/bin/celestia-appd-v3",
			assert: func(t *testing.T, a *Appd, env []string) {
				for _, e := range os.Environ() {
					require.Contains(t, env, e)
				}
				require.Contains(t, env, "CELESTIA_APPD_V3_TELEMETRY_PROMETHEUS_RETENTION_TIME=0")
			},
		},
		{
			name:   "overrides existing telemetry env var",
			path:   "/tmp/bin/celestia-appd-v3",
			setenv: map[string]string{"CELESTIA_APPD_V3_TELEMETRY_PROMETHEUS_RETENTION_TIME": "60"},
			assert: func(t *testing.T, a *Appd, env []string) {
				envKey := "CELESTIA_APPD_V3_TELEMETRY_PROMETHEUS_RETENTION_TIME"
				var matches []string
				for _, e := range env {
					if strings.HasPrefix(e, envKey+"=") {
						matches = append(matches, e)
					}
				}
				require.Len(t, matches, 2, "expected both the OS env value and the override")
				assert.Equal(t, envKey+"=60", matches[0], "OS env value should appear first")
				assert.Equal(t, envKey+"=0", matches[1], "override should appear last so it wins")
			},
		},
		{
			name:   "preserves custom env var",
			path:   "/tmp/bin/celestia-appd-v6",
			setenv: map[string]string{"CELESTIA_TEST_CUSTOM_VAR": "hello"},
			assert: func(t *testing.T, a *Appd, env []string) {
				require.Contains(t, env, "CELESTIA_TEST_CUSTOM_VAR=hello")
			},
		},
		{
			name: "telemetry vars appear at the end",
			path: "/tmp/bin/celestia-appd-v3",
			assert: func(t *testing.T, a *Appd, env []string) {
				osEnvLen := len(os.Environ())
				telemetryEnvs := a.telemetryDisableEnv()
				require.Equal(t, osEnvLen+len(telemetryEnvs), len(env))
				for i, te := range telemetryEnvs {
					assert.Equal(t, te, env[osEnvLen+i])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.setenv {
				t.Setenv(k, v)
			}
			a := &Appd{path: tt.path}
			env := a.getEnv()
			tt.assert(t, a, env)
		})
	}
}
