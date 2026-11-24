package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/expected"
	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/testchain"
)

func readRows(t *testing.T, path string) []string {
	bz, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(bz)), "\n")
	require.True(t, strings.HasPrefix(lines[0], "# fshdr"))
	return lines[1:]
}

func TestVerifyAndForwardAgree(t *testing.T) {
	c := testchain.New(t, 25)
	dir := t.TempDir()
	c.WriteStores(t, dir, c.N(), 22)

	verOut := filepath.Join(dir, "verify.tsv")
	anchor := fmt.Sprintf("%X", c.Header(25).Hash())
	require.NoError(t, cmdVerify([]string{"-chain-id", testchain.ChainID, "-blockstore", dir, "-from", "4", "-to", "20",
		"-anchor-height", "25", "-anchor-hash", anchor, "-window", "3", "-out", verOut}, nil))
	verRows := readRows(t, verOut)
	require.Len(t, verRows, 17)

	fwdOut := filepath.Join(dir, "forward.tsv")
	trusted := fmt.Sprintf("%X", c.Header(4).Hash())
	require.NoError(t, cmdForward([]string{"-chain-id", testchain.ChainID, "-data", dir, "-trusted-height", "4",
		"-trusted-hash", trusted, "-to", "20", "-all-signatures", "-out", fwdOut}, nil))
	require.Equal(t, verRows, readRows(t, fwdOut))

	for i, line := range verRows {
		r, err := expected.ParseRow(line)
		require.NoError(t, err)
		require.EqualValues(t, 4+i, r.Height)
		require.Equal(t, []byte(c.Header(r.Height+1).Hash()), r.NextHeaderHash)
	}

	var out bytes.Buffer
	require.NoError(t, cmdHeaders([]string{"-blockstore", dir, "-from", "4", "-to", "20"}, &out))
	require.Equal(t, verRows, strings.Split(strings.TrimSpace(out.String()), "\n")[1:])
}

func TestVerifyRejectsWrongAnchor(t *testing.T) {
	c := testchain.New(t, 10)
	dir := t.TempDir()
	c.WriteStores(t, dir, c.N(), 7)
	out := filepath.Join(dir, "x.tsv")
	err := cmdVerify([]string{"-chain-id", testchain.ChainID, "-blockstore", dir, "-from", "2", "-to", "8",
		"-anchor-height", "10", "-anchor-hash", fmt.Sprintf("%X", c.Header(9).Hash()), "-out", out}, nil)
	require.ErrorContains(t, err, "hash chain broken at height 10")
	_, statErr := os.Stat(out)
	require.True(t, os.IsNotExist(statErr), "no output on failure")
}

func TestForwardRejectsWrongTrustedHash(t *testing.T) {
	c := testchain.New(t, 10)
	dir := t.TempDir()
	c.WriteStores(t, dir, c.N(), 7)
	err := cmdForward([]string{"-chain-id", testchain.ChainID, "-data", dir, "-trusted-height", "3",
		"-trusted-hash", fmt.Sprintf("%X", c.Header(4).Hash()), "-to", "8"}, &bytes.Buffer{})
	require.ErrorContains(t, err, "trusted header 3 hash")
}

func TestFlagValidation(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"-chain-id", "c", "-from", "5", "-to", "4"},
		{"-chain-id", "c", "-to", "4", "-rpc", "http://x"},                                       // no trust root
		{"-chain-id", "c", "-to", "4", "-anchor-height", "4", "-anchor-hash", "AA", "-rpc", "x"}, // anchor not above to
	} {
		_, err := parseVerify(args)
		require.Error(t, err, args)
	}
}
