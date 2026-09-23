package main

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/stretchr/testify/require"
)

func TestRunRejectsInvalidPreencodeConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  config
		want string
	}{
		{"requires upload-only", config{preencode: true}, "requires --upload-only"},
		{"rejects download", config{preencode: true, uploadOnly: true, download: true}, "does not support --download"},
		{"negative size", config{preencode: true, uploadOnly: true, blobSize: -1}, "--blob-size"},
		{"empty", config{preencode: true, uploadOnly: true}, "--blob-size"},
		{"oversized", config{preencode: true, uploadOnly: true, blobSize: fibre.DefaultBlobConfigV0().MaxDataSize + 1}, "--blob-size"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.concurrency = 1
			require.ErrorContains(t, run(tc.cfg), tc.want)
		})
	}
}
