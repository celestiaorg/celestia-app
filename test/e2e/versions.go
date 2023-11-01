package e2e

import (
	"fmt"
	"math/rand"
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
	if v.Major > v2.Major {
		return true
	}
	if v.Major < v2.Major {
		return false
	}
	if v.Minor > v2.Minor {
		return true
	}
	if v.Minor < v2.Minor {
		return false
	}
	if v.Patch > v2.Patch {
		return true
	}
	if v.Patch < v2.Patch {
		return false
	}
	if !v.IsRC && v2.IsRC {
		return true
	}
	if v.IsRC && !v2.IsRC {
		return false
	}
	if v.RC > v2.RC {
		return true
	}
	return false
}

type VersionSet []Version

func ParseVersions(versionStr string) VersionSet {
	versions := strings.Split(versionStr, "\n")
	output := make(VersionSet, 0, len(versions))
	for _, v := range versions {
		var major, minor, patch, rc uint64
		isRC := false
		if strings.Contains(v, "rc") {
			_, err := fmt.Sscanf(v, "v%d.%d.%d-rc%d", &major, &minor, &patch, &rc)
			isRC = true
			if err != nil {
				continue
			}
		} else {
			_, err := fmt.Sscanf(v, "v%d.%d.%d", &major, &minor, &patch)
			if err != nil {
				continue
			}
		}
		output = append(output, Version{major, minor, patch, isRC, rc})
	}
	return output
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
	return v[r.Intn(len(v))]
}

func (v VersionSet) String() string {
	output := make([]string, len(v))
	for i, version := range v {
		output[i] = version.String()
	}
	return strings.Join(output, "\t")
}
