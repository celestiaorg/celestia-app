package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitSSHPubKeyPath(t *testing.T) {
	t.Setenv(EnvVarPubSSHKeyPath, "/env.pub")

	tests := map[string]struct {
		dotenv bool
		args   []string
		want   string
	}{
		"env without .env file": {false, nil, "/env.pub"},
		"flag overrides env":    {true, []string{"--ssh-pub-key-path", "/flag.pub"}, "/flag.pub"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.dotenv {
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("PROVIDER=digitalocean\n"), 0o600))
			}
			cmd := initCmd()
			cmd.SetArgs(append([]string{"-d", dir, "-r", "../..", "--chainID", "test", "--experiment", "test"}, tt.args...))
			require.NoError(t, cmd.Execute())

			cfg, err := LoadConfig(dir)
			require.NoError(t, err)
			require.Equal(t, tt.want, cfg.SSHPubKeyPath)
		})
	}
}
