package cmd

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryScriptCmdPrintsScript(t *testing.T) {
	want, err := os.ReadFile("../../../scripts/hardfork/recovery.sh")
	require.NoError(t, err)

	cmd := recoveryScriptCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(nil)
	require.NoError(t, cmd.Execute())
	require.Equal(t, string(want), out.String())
}
