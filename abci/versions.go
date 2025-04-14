package abci

import (
	"fmt"
	"sort"

	"github.com/01builders/nova/appd"
)

// Version defines the configuration for remote apps.
type Version struct {
	AppVersion  uint64
	ABCIVersion ABCIClientVersion
	Appd        *appd.Appd
	PreHandlers []string // Commands to run before starting the app
	StartArgs   []string // Extra arguments to pass to the app
}

type Versions []Version

// Sorted returns a sorted slice of Versions, sorted by AppVersion (ascending).
func (v Versions) Sorted() Versions {
	// convert map to slice
	versionList := make([]Version, 0)
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

// GetStartArgs returns the appropriate args.
func (v Version) GetStartArgs(args []string) []string {
	if len(v.StartArgs) > 0 {
		return append(args, v.StartArgs...)
	}

	// Default flags for standalone apps.
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
