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
		{"rejects download", config{preencode: true, uploadOnly: true, download: true}, "does not support --download"},
		{"negative size", config{preencode: true, uploadOnly: true, blobSize: -1}, "--blob-size"},
		{"empty", config{preencode: true, uploadOnly: true}, "--blob-size"},
		{"oversized", config{preencode: true, uploadOnly: true, blobSize: fibre.DefaultBlobConfigV0().MaxDataSize + 1}, "--blob-size"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.concurrency = 1
			for _, uploadOnly := range []bool{false, true} {
				tc.cfg.uploadOnly = uploadOnly
				require.ErrorContains(t, run(tc.cfg), tc.want)
			}
		})
	}
}

func TestRunRejectsInvalidFreshBlobSize(t *testing.T) {
	for _, size := range []int{-1, 0, fibre.DefaultBlobConfigV0().MaxDataSize + 1} {
		for _, uploadOnly := range []bool{false, true} {
			require.ErrorContains(t, run(config{concurrency: 1, blobSize: size, uploadOnly: uploadOnly}), "--blob-size")
		}
	}
}
