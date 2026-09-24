package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFibreTxsimPreencode(t *testing.T) {
	for _, onEncoders := range []bool{false, true} {
		for _, preencode := range []bool{false, true} {
			t.Run(fmt.Sprintf("encoders=%t/preencode=%t", onEncoders, preencode), func(t *testing.T) {
				dir := t.TempDir()
				capture := filepath.Join(dir, "ssh-args")
				t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
				t.Setenv("SSH_CAPTURE", capture)
				require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$SSH_CAPTURE\"\n"), 0o700))
				cfg := NewConfig("test", "test", "")
				cfg.Validators = []Instance{{Name: "validator-0", PublicIP: "192.0.2.1"}}
				cfg.Encoders = []Instance{{Name: "encoder-0", PublicIP: "192.0.2.2"}}
				require.NoError(t, cfg.SaveFile(filepath.Join(dir, "config.json")))
				cmd := fibreTxsimCmd()
				cmd.SetArgs([]string{"--directory", dir, fmt.Sprintf("--on-encoders=%t", onEncoders), fmt.Sprintf("--preencode=%t", preencode)})
				require.NoError(t, cmd.Execute())
				args, err := os.ReadFile(capture)
				require.NoError(t, err)
				parts := strings.SplitN(string(args), "\"", 3)
				require.Len(t, parts, 3)
				script, err := base64.StdEncoding.DecodeString(parts[1])
				require.NoError(t, err)
				require.Contains(t, string(script), fmt.Sprintf("--preencode=%t", preencode))
				require.Contains(t, string(script), "--upload-only=false")
			})
		}
	}
}
