package testnet

import (
	"fmt"
	"math/rand"
	"os/exec"
	"sort"
	"strings"
)

type Version struct {
	Major uint64
	Minor uint64
	Patch uint64
	IsRC  bool
	RC    uint64
}

func (v Version) String() string {
	if v.IsRC {
		return fmt.Sprintf("v%d.%d.%d-rc%d", v.Major, v.Minor, v.Patch, v.RC)
	}
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

func (v Version) IsGreater(v2 Version) bool {
	if v.Major != v2.Major {
		return v.Major > v2.Major
	}
	if v.Minor != v2.Minor {
		return v.Minor > v2.Minor
	}
	if v.Patch != v2.Patch {
		return v.Patch > v2.Patch
	}
	if v.IsRC != v2.IsRC {
		return !v.IsRC
	}
	return v.RC > v2.RC
}

type VersionSet []Version

// ParseVersions takes a string of space-separated versions and returns a
// VersionSet. Invalid versions are ignored.
func ParseVersions(versionStr string) VersionSet {
	versions := strings.Split(versionStr, " ")
	output := make(VersionSet, 0, len(versions))
	for _, v := range versions {
		version, isValid := ParseVersion(v)
		if !isValid {
			continue
		}
		output = append(output, version)
	}
	return output
}

// ParseVersion takes a string and returns a Version. If the string is not a
// valid version, the second return value is false.
// Must be of the format v1.0.0 or v1.0.0-rc1 (i.e. following SemVer)
func ParseVersion(version string) (Version, bool) {
	var major, minor, patch, rc uint64
	isRC := false
	if strings.Contains(version, "rc") {
		_, err := fmt.Sscanf(version, "v%d.%d.%d-rc%d", &major, &minor, &patch, &rc)
		isRC = true
		if err != nil {
			return Version{}, false
		}
	} else {
		_, err := fmt.Sscanf(version, "v%d.%d.%d", &major, &minor, &patch)
		if err != nil {
			return Version{}, false
		}
	}
	return Version{major, minor, patch, isRC, rc}, true
}

// GetLatestVersion retrieves the latest git commit hash
// or semantic version of the main branch.
func GetLatestVersion() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--short", "main")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git commit hash: %v", err)
	}
	latestVersion := string(output)

	_, isSemVer := ParseVersion(latestVersion)
	switch {
	case isSemVer:
		return latestVersion, nil
	case latestVersion == "latest":
		return latestVersion, nil
	case len(latestVersion) == 7:
		return latestVersion, nil
	case len(latestVersion) >= 8:
		// assume this is a git commit hash (we need to trim the last digit to match the docker image tag)
		return latestVersion[:7], nil
	default:
		return "", fmt.Errorf("unrecognised version %s", latestVersion)
	}
}

func (v VersionSet) FilterMajor(majorVersion uint64) VersionSet {
	output := make(VersionSet, 0, len(v))
	for _, version := range v {
		if version.Major == majorVersion {
			output = append(output, version)
		}
	}
	return output
}

func (v VersionSet) FilterOutReleaseCandidates() VersionSet {
	output := make(VersionSet, 0, len(v))
	for _, version := range v {
		if version.IsRC {
			continue
		}
		output = append(output, version)
	}
	return output
}

func (v VersionSet) GetLatest() Version {
	latest := Version{}
	for _, version := range v {
		if version.IsGreater(latest) {
			latest = version
		}
	}
	return latest
}

func (v VersionSet) Order() {
	sort.Slice(v, func(i, j int) bool {
		return v[j].IsGreater(v[i])
	})
}

func (v VersionSet) Random(r *rand.Rand) Version {
	if len(v) == 0 {
		panic("there are no versions to pick from")
	}
	return v[r.Intn(len(v))]
}

func (v VersionSet) String() string {
	output := make([]string, len(v))
	for i, version := range v {
		output[i] = version.String()
	}
	return strings.Join(output, "\t")
}
