package cmd_test

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/cmd/celestia-appd/cmd"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestAddrConversionCmd(t *testing.T) {
	addr := "celestia1grvklux2yjsln7ztk6slv538396qatckqhs86z"
	valAddr := "celestiavaloper1grvklux2yjsln7ztk6slv538396qatck9gj7vy"
	t.Run("returns an ordinary address", func(t *testing.T) {
		output, err := executeCmd(cmd.AddrConversionCmd(), addr)
		assert.NoError(t, err)
		assert.Equal(t, valAddr, output)
	})
	t.Run("converts a valoper address", func(t *testing.T) {
		output, err := executeCmd(cmd.AddrConversionCmd(), valAddr)
		assert.NoError(t, err)
		assert.Equal(t, addr, output)
	})
	t.Run("returns an error for an invalid address", func(t *testing.T) {
		invalidAddr := "celestia1xxxxxxxxxxxx"
		_, err := executeCmd(cmd.AddrConversionCmd(), invalidAddr)
		assert.Error(t, err)
		assert.ErrorContains(t, err, "invalid address")
	})
}

func executeCmd(cmd *cobra.Command, args ...string) (string, error) {
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}
