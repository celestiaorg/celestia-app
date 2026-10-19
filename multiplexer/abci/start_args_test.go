package abci

import (
	"strconv"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestGetStartArgsPreservesChildDefaults(t *testing.T) {
	for _, explicitlySet := range []bool{false, true} {
		t.Run("explicit="+strconv.FormatBool(explicitlySet), func(t *testing.T) {
			parent := pflag.NewFlagSet("parent", pflag.ContinueOnError)
			parent.String("moniker", "native", "")
			parent.Bool("trace", true, "")
			parent.StringSlice("origins", []string{"native"}, "")
			if explicitlySet {
				require.NoError(t, parent.Parse([]string{"--moniker=native", "--trace=true", "--origins=native"}))
			}
			child := pflag.NewFlagSet("child", pflag.ContinueOnError)
			moniker := child.String("moniker", "embedded", "")
			trace := child.Bool("trace", false, "")
			origins := child.StringSlice("origins", []string{"embedded"}, "")
			child.String("transport", "", "")
			require.NoError(t, child.Parse((Version{StartArgs: []string{"--transport=grpc"}}).GetStartArgs(parent)))
			if explicitlySet {
				require.Equal(t, "native", *moniker)
				require.True(t, *trace)
				require.Equal(t, []string{"native"}, *origins)
			} else {
				require.Equal(t, "embedded", *moniker)
				require.False(t, *trace)
				require.Equal(t, []string{"embedded"}, *origins)
			}
		})
	}
}

func TestGetStartArgsMandatoryOverridesWin(t *testing.T) {
	for _, cacheEnabled := range []bool{false, true} {
		t.Run("cache="+strconv.FormatBool(cacheEnabled), func(t *testing.T) {
			newFlags := func() *pflag.FlagSet {
				flags := pflag.NewFlagSet("start", pflag.ContinueOnError)
				flags.Bool("inter-block-cache", false, "")
				flags.Bool("with-tendermint", true, "")
				flags.String("transport", "socket", "")
				return flags
			}
			parent := newFlags()
			require.NoError(t, parent.Parse([]string{"--inter-block-cache=" + strconv.FormatBool(!cacheEnabled), "--with-tendermint=true", "--transport=socket"}))
			version := Version{StartArgs: []string{"--inter-block-cache=" + strconv.FormatBool(cacheEnabled), "--with-tendermint=false", "--transport=grpc"}}
			child := newFlags()
			require.NoError(t, child.Parse(version.GetStartArgs(parent)))
			cache, err := child.GetBool("inter-block-cache")
			require.NoError(t, err)
			require.Equal(t, cacheEnabled, cache)
			comet, err := child.GetBool("with-tendermint")
			require.NoError(t, err)
			require.False(t, comet)
			transport, err := child.GetString("transport")
			require.NoError(t, err)
			require.Equal(t, "grpc", transport)
		})
	}
}

func TestGetStartArgsReusesFlagsAcrossVersions(t *testing.T) {
	parent := pflag.NewFlagSet("start", pflag.ContinueOnError)
	parent.String("home", "", "")
	parent.Bool("bypass-config-overrides", false, "")
	parent.StringSlice("origins", nil, "")
	require.NoError(t, parent.Parse([]string{"--home=start", "--bypass-config-overrides", `--origins="a,b",c`}))
	older := Version{StartArgs: []string{"--transport=grpc"}, UnsupportedFlags: map[string]struct{}{"bypass-config-overrides": {}}}
	newer := Version{StartArgs: []string{"--transport=grpc"}}
	oldArgs := older.GetStartArgs(parent)
	require.Equal(t, []string{"--home=start", `--origins="a,b",c`, "--transport=grpc"}, oldArgs)
	// Filtering for an earlier binary must not consume flags needed after an upgrade.
	require.Equal(t, []string{"--bypass-config-overrides=true", "--home=start", `--origins="a,b",c`, "--transport=grpc"}, newer.GetStartArgs(parent))
	require.Equal(t, oldArgs, older.GetStartArgs(parent))
	// Each result owns its arguments; callers must not mutate the version's overrides.
	oldArgs[len(oldArgs)-1] = "--transport=socket"
	require.Equal(t, []string{"--transport=grpc"}, older.StartArgs)
	require.Equal(t, "--transport=grpc", older.GetStartArgs(parent)[2])
}
