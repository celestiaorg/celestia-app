package fibre

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadNetworkConfig(t *testing.T) {
	valid := `{"source_ips":["127.0.0.1"],"validators":{"` + strings.Repeat("AB", 20) + `":["127.0.0.1:7980"]}}`
	for _, tc := range []struct {
		name, data string
		valid      bool
	}{
		{"valid", valid, true},
		{"unknown", strings.Replace(valid, "source_ips", "source_ip", 1), false},
		{"trailing", valid + `{}`, false},
		{"null", `null`, false},
		{"oversized", strings.Repeat(" ", (1<<20)+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "network.json")
			require.NoError(t, os.WriteFile(path, []byte(tc.data), 0o600))
			cfg, err := LoadNetworkConfig(path)
			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []string{"127.0.0.1"}, cfg.SourceIPs)
		})
	}
}
