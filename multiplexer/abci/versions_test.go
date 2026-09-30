package abci

import (
	"errors"
	"fmt"
<<<<<<< HEAD
=======
	"math"
	"slices"
>>>>>>> 6f8c6b1 (fix(multiplexer): don't forward unsupported start flags to embedded binaries (#7991))
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestGetForAppVersion(t *testing.T) {
	tests := []struct {
		name        string
		versions    Versions
		appVersion  uint64
		expected    Version
		expectedErr error
	}{
		{
			name: "exact match",
			versions: Versions{
				{AppVersion: 1},
				{AppVersion: 2},
				{AppVersion: 3},
			},
			appVersion:  2,
			expected:    Version{AppVersion: 2},
			expectedErr: nil,
		},
		{
			name: "no matching version returns smallest available version",
			versions: Versions{
				{AppVersion: 2},
				{AppVersion: 3},
			},
			appVersion:  1,
			expected:    Version{AppVersion: 2},
			expectedErr: nil,
		},
		{
			name:        "empty versions list returns error",
			versions:    Versions{},
			appVersion:  1,
			expected:    Version{},
			expectedErr: fmt.Errorf("%w: %d", ErrNoVersionFound, 1),
		},
		{
			name: "app version matches the lowest version",
			versions: Versions{
				{AppVersion: 1},
				{AppVersion: 3},
			},
			appVersion:  1,
			expected:    Version{AppVersion: 1},
			expectedErr: nil,
		},
		{
			name: "app version not in list, returns lowest",
			versions: Versions{
				{AppVersion: 4},
				{AppVersion: 5},
				{AppVersion: 6},
			},
			appVersion:  2,
			expected:    Version{AppVersion: 4},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := tt.versions.GetForAppVersion(tt.appVersion)

			if tt.expectedErr != nil {
				require.Error(t, err)
				require.EqualError(t, tt.expectedErr, err.Error(), "unexpected error message")
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, actual, "unexpected result")
			}
		})
	}
}

func TestShouldUseLatestApp(t *testing.T) {
	tests := []struct {
		name       string
		versions   Versions
		appVersion uint64
		expected   bool
	}{
		{"No versions available", Versions{}, 1, true},
		{
			"App version matches the first version",
			Versions{
				{AppVersion: 1},
				{AppVersion: 2},
			},
			1, false,
		},
		{
			"App version matches a later version",
			Versions{
				{AppVersion: 1},
				{AppVersion: 2},
			},
			2, false,
		},
		{
			"App version does not match any version",
			Versions{
				{AppVersion: 1},
				{AppVersion: 2},
			},
			3, true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.versions.ShouldUseLatestApp(tt.appVersion))
		})
	}
}

func TestEnsureUniqueVersions(t *testing.T) {
	tests := []struct {
		name        string
		versions    Versions
		expectedErr error
	}{
		{
			name:        "no duplicates",
			versions:    []Version{{AppVersion: 1}, {AppVersion: 2}, {AppVersion: 3}},
			expectedErr: nil,
		},
		{
			name:        "duplicate app versions",
			versions:    []Version{{AppVersion: 1}, {AppVersion: 2}, {AppVersion: 1}},
			expectedErr: errors.New("version 1 specified multiple times"),
		},
		{
			name:        "empty list",
			versions:    []Version{},
			expectedErr: errors.New("no versions specified"),
		},
		{
			name:        "single element",
			versions:    []Version{{AppVersion: 1}},
			expectedErr: nil,
		},
		{
			name:        "multiple duplicates",
			versions:    []Version{{AppVersion: 1}, {AppVersion: 2}, {AppVersion: 1}, {AppVersion: 3}, {AppVersion: 2}},
			expectedErr: errors.New("version 1 specified multiple times"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.versions.Validate()

			if tt.expectedErr != nil {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedErr.Error(), "expected error message mismatch")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetStartArgs(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"defaults are not forwarded", []string{"start"}, nil},
		{"supported flag", []string{"start", "--home", "foo"}, []string{"--home=foo"}},
		{"separate unsupported value", []string{"start", "--otel-endpoint", "localhost:4318", "--home", "foo"}, []string{"--home=foo"}},
		{"inline unsupported value", []string{"start", "--otel-endpoint=localhost:4318", "--home=foo"}, []string{"--home=foo"}},
		{"unsupported bool", []string{"start", "--fibre-promise-cache", "--home", "foo"}, []string{"--home=foo"}},
		{"unsupported false bool", []string{"start", "--fibre-promise-cache=false", "--home", "foo"}, []string{"--home=foo"}},
		{"flag-shaped value", []string{"start", "--moniker", "--otel-endpoint", "--home", "/tmp/node"}, []string{"--home=/tmp/node", "--moniker=--otel-endpoint"}},
		{"start value before subcommand", []string{"--home", "start", "--log_level", "info", "start"}, []string{"--home=start", "--log_level=info"}},
		{"start value after subcommand", []string{"start", "--moniker", "start"}, []string{"--moniker=start"}},
		{"explicit booleans", []string{"start", "--inter-block-cache=false", "--trace"}, []string{"--inter-block-cache=false", "--trace=true"}},
		{"empty value and shorthand", []string{"start", "-m", "", "--home="}, []string{"--home=", "--moniker="}},
		{"last scalar value wins", []string{"start", "--home=first", "--home=last"}, []string{"--home=last"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			version := Version{
				StartArgs:        []string{"--inter-block-cache=true", "--transport=grpc"},
				UnsupportedFlags: map[string]struct{}{"otel-endpoint": {}, "fibre-promise-cache": {}},
			}
			root := &cobra.Command{Use: "celestia-appd"}
			root.PersistentFlags().String("home", "default-home", "")
			root.PersistentFlags().String("log_level", "info", "")
			cmd := &cobra.Command{Use: "start", RunE: func(cmd *cobra.Command, _ []string) error {
				want := slices.Concat(test.want, version.StartArgs)
				require.Equal(t, want, version.GetStartArgs(cmd.Flags()))
				return nil
			}}
			cmd.Flags().StringP("moniker", "m", "default-moniker", "")
			cmd.Flags().String("otel-endpoint", "", "")
			cmd.Flags().Bool("fibre-promise-cache", true, "")
			cmd.Flags().Bool("inter-block-cache", true, "")
			cmd.Flags().Bool("trace", false, "")
			root.AddCommand(cmd)
			root.SetArgs(test.input)
			require.NoError(t, root.Execute())
		})
	}
}

func TestGetStartArgsSlices(t *testing.T) {
	newFlags := func() *pflag.FlagSet {
		flags := pflag.NewFlagSet("start", pflag.ContinueOnError)
		flags.StringSlice("api.enabled-unsafe-cors-origins", []string{"default"}, "")
		flags.StringArray("array", nil, "")
		return flags
	}
	for _, input := range [][]string{
		{"--api.enabled-unsafe-cors-origins=a,b", "--api.enabled-unsafe-cors-origins=\"c,d\",e", "--array=a,b", "--array=\"quoted\""},
		{"--api.enabled-unsafe-cors-origins=", "--array="},
		{`--api.enabled-unsafe-cors-origins=""`},
	} {
		parent := newFlags()
		require.NoError(t, parent.Parse(input))
		version := Version{StartArgs: []string{"--transport=grpc"}}
		args := version.GetStartArgs(parent)
		child := newFlags()
		child.String("transport", "", "")
		require.NoError(t, child.Parse(args))
		parent.Visit(func(flag *pflag.Flag) {
			require.Equal(t, flag.Value.(pflag.SliceValue).GetSlice(), child.Lookup(flag.Name).Value.(pflag.SliceValue).GetSlice())
		})
	}
}

func TestGetStartArgsDefaultOverrides(t *testing.T) {
	require.Equal(t, []string{"--grpc.enable", "--api.enable", "--api.swagger=false", "--with-tendermint=false", "--transport=grpc"}, (Version{}).GetStartArgs(nil))
}
