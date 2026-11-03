package appd

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMPTCPListenersDisabledByDefault(t *testing.T) {
	t.Setenv("GODEBUG", "")
	var lc net.ListenConfig
	require.False(t, lc.MultipathTCP())
}

func TestMPTCPDisableEnv(t *testing.T) {
	tests := map[string]struct {
		godebug string
		want    []string
	}{
		"unset":               {"", []string{"GODEBUG=multipathtcp=0"}},
		"other settings kept": {"http2client=0", []string{"GODEBUG=http2client=0,multipathtcp=0"}},
		"operator override":   {"multipathtcp=1", nil},
		"key with space":      {" multipathtcp=1", []string{"GODEBUG= multipathtcp=1,multipathtcp=0"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GODEBUG", tt.godebug)
			require.Equal(t, tt.want, mptcpDisableEnv())
		})
	}
}

func TestChildEnvDisablesMPTCP(t *testing.T) {
	t.Setenv("GODEBUG", "")
	a := &Appd{path: "/bin/true"}

	env := a.getEnv()
	require.Equal(t, "GODEBUG=multipathtcp=0", env[len(env)-1])

	cmd, err := a.CreateExecCommand()
	require.NoError(t, err)
	require.Equal(t, "GODEBUG=multipathtcp=0", cmd.Env[len(cmd.Env)-1])
}
