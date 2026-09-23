package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// TestConfigFlag verifies that commands load the file named by --config
// instead of always loading config.json.
func TestConfigFlag(t *testing.T) {
	t.Setenv(EnvVarDigitalOceanToken, "")
	t.Setenv(EnvVarGoogleCloudProject, "")
	t.Setenv(EnvVarAWSRegion, "")
	t.Setenv("AWS_MAX_ATTEMPTS", "1")

	tests := []struct {
		name    string
		cmd     func() *cobra.Command
		args    []string
		wantErr string
	}{
		{"up", upCmd, nil, "no validators found in config"},
		{"deploy", deployCmd, []string{"--skip-payload-upload"}, "no validators found in config"},
		{"down", downCmd, nil, "no validators found in config"},
		{"list", listCmd, nil, "no cloud provider credentials found"},
		{"reset", resetCmd, nil, "no validators found in config"},
		{"download", downloadCmd, nil, "no validators (nodes) found in config"},
		{"txsim", startTxsimCmd, []string{"--instances", "1", "--sequences", "1"}, "no validators found in config"},
		{"kill-session", killTmuxSessionCmd, []string{"--session", "s"}, "no validators found in config"},
		{"download s3", downloadS3DataCmd, nil, "failed to download S3 objects"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			// Static creds and a local endpoint keep `download s3` offline.
			cfg := NewConfig("test", "test", "").WithS3Config(S3Config{
				Region:          "us-east-1",
				AccessKeyID:     "id",
				SecretAccessKey: "secret",
				BucketName:      "bucket",
				Endpoint:        "http://127.0.0.1:1",
			})
			require.NoError(t, cfg.SaveFile(filepath.Join(dir, "other.json")))

			cmd := tt.cmd()
			cmd.SetArgs(append([]string{"-d", dir, "-c", "other.json"}, tt.args...))
			cmd.SilenceUsage = true
			err := cmd.Execute()
			require.NotErrorIs(t, err, os.ErrNotExist)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestSaveFileLoadConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "encoder-only.json")
	cfg := NewConfig("exp", "chain", "")
	require.NoError(t, cfg.SaveFile(path))

	got, err := LoadConfigFile(path)
	require.NoError(t, err)
	require.Equal(t, cfg.ChainID, got.ChainID)
}
