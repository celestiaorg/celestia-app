package abci

import (
	"errors"
	"fmt"
	"sort"

	"github.com/celestiaorg/celestia-app/v10/multiplexer/appd"
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
}

type Versions []Version

// Sorted returns a sorted slice of Versions, sorted by AppVersion (ascending).
func (v Versions) Sorted() Versions {
	versionList := make(Versions, len(v))
	copy(versionList, v)

	sort.SliceStable(versionList, func(i, j int) bool {
		return versionList[i].AppVersion < versionList[j].AppVersion
	})

	return versionList
}

// GetForAppVersion returns the version registered for exactly appVersion. It
// returns ErrNoVersionFound for newer versions and ErrUnsupportedAppVersion
// when no registered binary can serve appVersion.
func (v Versions) GetForAppVersion(appVersion uint64) (Version, error) {
	if len(v) == 0 {
		return Version{}, fmt.Errorf("%w for app version %d: no versions registered", ErrNoVersionFound, appVersion)
	}

	for _, version := range v {
		if version.AppVersion == appVersion {
			return version, nil
		}
	}

	lowest, highest := v.bounds()
	if appVersion > highest {
		return Version{}, fmt.Errorf("%w for app version %d: highest registered version is %d", ErrNoVersionFound, appVersion, highest)
	}
	return Version{}, fmt.Errorf("%w %d: registered versions are %d through %d", ErrUnsupportedAppVersion, appVersion, lowest, highest)
}

// ShouldUseLatestApp returns true if appVersion is newer than every registered
// version and so must be served by the native (latest) app.
func (v Versions) ShouldUseLatestApp(appVersion uint64) bool {
	_, err := v.GetForAppVersion(appVersion)
	return errors.Is(err, ErrNoVersionFound)
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

// Validate checks that versions is non-empty, has no duplicate app versions,
// and forms a contiguous range of app versions.
func (v Versions) Validate() error {
	if len(v) == 0 {
		return fmt.Errorf("no versions specified")
	}

	seen := make(map[uint64]struct{}, len(v))
	for _, ver := range v {
		if _, exists := seen[ver.AppVersion]; exists {
			return fmt.Errorf("version %d specified multiple times", ver.AppVersion)
		}
		seen[ver.AppVersion] = struct{}{}
	}

	lowest, highest := v.bounds()
	for want := lowest; ; want++ {
		if _, ok := seen[want]; !ok {
			return fmt.Errorf("version %d is missing: registered app versions must be contiguous (%d through %d)", want, lowest, highest)
		}
		if want == highest {
			break
		}
	}

	return nil
}

// bounds returns the lowest and highest app version in v, which must be
// non-empty.
func (v Versions) bounds() (lowest, highest uint64) {
	lowest, highest = v[0].AppVersion, v[0].AppVersion
	for _, ver := range v[1:] {
		lowest = min(lowest, ver.AppVersion)
		highest = max(highest, ver.AppVersion)
	}
	return lowest, highest
}
