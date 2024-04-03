package cmd_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/cmd/celestia-appd/cmd"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestConvert(t *testing.T) {
	type testCase struct {
		input   string
		want    string
		wantErr error
	}
	testCases := []testCase{
		{"celestia1grvklux2yjsln7ztk6slv538396qatckqhs86z", "celestiavaloper1grvklux2yjsln7ztk6slv538396qatck9gj7vy", nil},
		{"celestiavaloper1grvklux2yjsln7ztk6slv538396qatck9gj7vy", "celestia1grvklux2yjsln7ztk6slv538396qatckqhs86z", nil},
		{"celestiavaloper1xxxxxxxxx", "", fmt.Errorf("invalid address: celestiavaloper1xxxxxxxxx")},
	}
	for _, tc := range testCases {
		got, err := cmd.Convert(tc.input)
		if tc.wantErr != nil {
			assert.Error(t, err)
			return
		}
		assert.Equal(t, tc.want, got)
	}
}

func TestAddrConversionCmd(t *testing.T) {
	t.Run("returns an error for an invalid address", func(t *testing.T) {
		invalidAddr := "celestia1xxxxxxxxxxxx"
		cmd := cmd.AddrConversionCmd()
		_, err := executeCmd(cmd, invalidAddr)
		assert.Error(t, err)
		assert.ErrorContains(t, err, "invalid address")
	})
	// can't test a successful conversion because the command output is sent to
	// stdout and not returned even with a custom executeCmd function
}

func executeCmd(cmd *cobra.Command, args ...string) (string, error) {
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}
