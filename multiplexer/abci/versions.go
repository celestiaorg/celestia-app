package abci

import (
<<<<<<< HEAD
=======
	"bytes"
	"encoding/csv"
	"errors"
>>>>>>> 6f8c6b1 (fix(multiplexer): don't forward unsupported start flags to embedded binaries (#7991))
	"fmt"
	"sort"
	"strings"

	"github.com/celestiaorg/celestia-app/v10/multiplexer/appd"
	"github.com/spf13/pflag"
)

// NewVersions returns a list of versions sorted by app version.
func NewVersions(v ...Version) (Versions, error) {
	versions := Versions(v)
	if err := versions.Validate(); err != nil {
		return nil, err
	}
	return versions.Sorted(), nil
}

// Version defines the configuration for remote apps.
type Version struct {
	AppVersion  uint64
	ABCIVersion ABCIClientVersion
	Appd        *appd.Appd
	PreHandlers []string // Commands to run before starting the app
	StartArgs   []string // Extra arguments to pass to the app
	// UnsupportedFlags are operator flags this app doesn't define, so they are
	// not forwarded to it.
	UnsupportedFlags map[string]struct{}
}

type Versions []Version

// Sorted returns a sorted slice of Versions, sorted by AppVersion (ascending).
func (v Versions) Sorted() Versions {
	// convert map to slice
	versionList := make([]Version, 0, len(v))
	for _, ver := range v {
		versionList = append(versionList, ver)
	}

	// sort by AppVersion in ascending order
	sort.SliceStable(versionList, func(i, j int) bool {
		return versionList[i].AppVersion < versionList[j].AppVersion
	})

	return versionList
}

// GetForAppVersion returns the version for a given appVersion.
// if the app version specified is lower than the minimum app version, return the lowest version.
func (v Versions) GetForAppVersion(appVersion uint64) (Version, error) {
	if len(v) == 0 {
		return Version{}, fmt.Errorf("%w: %d", ErrNoVersionFound, appVersion)
	}

	lowestVersion := v[0]
	highestVersion := v[len(v)-1]

	// the version being specified is higher than any version we have, we assume this is the latest version.
	if appVersion > highestVersion.AppVersion {
		return Version{}, fmt.Errorf("%w: %d", ErrNoVersionFound, appVersion)
	}

	for _, version := range v {
		if version.AppVersion == appVersion {
			return version, nil
		}
	}

	// return the lowest version if the exact version is not found.
	return lowestVersion, nil
}

// ShouldUseLatestApp returns true if there is no version found with the given appVersion.
func (v Versions) ShouldUseLatestApp(appVersion uint64) bool {
	// should only use the latest app if there are no versions to use based on desired version.
	_, err := v.GetForAppVersion(appVersion)
	return err != nil
}

// GetStartArgs forwards only explicitly set, supported flags. Cobra has already
// parsed their values, so strings such as "start" or "--otel-endpoint" cannot
// be mistaken for subcommands or flags. Mandatory child overrides come last.
func (v Version) GetStartArgs(flags *pflag.FlagSet) []string {
	args := []string{}
	if flags != nil {
		flags.Visit(func(flag *pflag.Flag) {
			if _, unsupported := v.UnsupportedFlags[flag.Name]; unsupported {
				return
			}
			if slice, ok := flag.Value.(pflag.SliceValue); ok {
				values := slice.GetSlice()
				if flag.Value.Type() == "stringArray" {
					for _, value := range values {
						args = append(args, "--"+flag.Name+"="+value)
					}
					return
				}
				// SliceValue.String includes brackets and is not a CLI value. CSV
				// preserves commas and quotes in stringSlice elements.
				var value bytes.Buffer
				writer := csv.NewWriter(&value)
				_ = writer.Write(values)
				writer.Flush()
				encoded := strings.TrimSuffix(value.String(), "\n")
				if len(values) == 1 && values[0] == "" {
					encoded = `""` // distinguish one empty string from an empty slice
				}
				args = append(args, "--"+flag.Name+"="+encoded)
				return
			}
			args = append(args, "--"+flag.Name+"="+flag.Value.String())
		})
	}
	if len(v.StartArgs) > 0 {
		return append(args, v.StartArgs...)
	}
	return append(args,
		"--grpc.enable",
		"--api.enable",
		"--api.swagger=false",
		"--with-tendermint=false",
		"--transport=grpc",
	)
}

// Validate checks for duplicate app versions in a slice of Versions.
func (v Versions) Validate() error {
	if len(v) == 0 {
		return fmt.Errorf("no versions specified")
	}

	seen := make(map[uint64]struct{})
	for _, ver := range v {
		if _, exists := seen[ver.AppVersion]; exists {
			return fmt.Errorf("version %d specified multiple times", ver.AppVersion)
		}
		seen[ver.AppVersion] = struct{}{}
	}

	return nil
}
