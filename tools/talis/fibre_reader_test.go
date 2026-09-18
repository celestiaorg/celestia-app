package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFibreReaderReadsPerBlobDefault(t *testing.T) {
	cmd := fibreReaderCmd()
	reads, err := cmd.Flags().GetInt("reads-per-blob")
	require.NoError(t, err)
	require.Equal(t, 1, reads)
}

func TestFibreReaderRejectsInvalidReadsPerBlob(t *testing.T) {
	for _, reads := range []int{0, -1} {
		t.Run(fmt.Sprint(reads), func(t *testing.T) {
			cmd := fibreReaderCmd()
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs([]string{"--reads-per-blob", fmt.Sprint(reads), "--directory", t.TempDir()})
			require.EqualError(t, cmd.Execute(), fmt.Sprintf("--reads-per-blob must be >= 1, got %d", reads))
		})
	}
}
