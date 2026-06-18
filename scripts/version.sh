#!/usr/bin/env bash
#
# Resolves the semantic version of the current git checkout. The result is
# embedded into the celestia-appd binary via the VERSION variable in the
# Makefile (and the cosmos-sdk version ldflags).
#
# A single release commit is usually tagged once per network, e.g. v3.8.1,
# v3.8.1-mocha and v3.8.1-arabica all point at the same commit. When several
# tags point at HEAD we must choose deterministically:
#
#   1. the base mainnet tag (vX.Y.Z) if present, so mainnet builds report a
#      clean version (preserves the behavior of #5894), otherwise
#   2. the network / pre-release tag, preferring mocha > arabica > rc > beta >
#      alpha, so a build from e.g. v7.0.2-mocha reports 7.0.2-mocha instead of
#      an arbitrary sibling tag (fixes #4631).
#
# `git describe` and `git name-rev` cannot be relied on here: their tie-breaking
# between tags on the same commit depends on tag type, creation order and tagger
# date, so they non-deterministically return the wrong network suffix (the old
# `cut -d'-' -f1` workaround instead dropped the suffix entirely).
#
# When HEAD is not exactly at a tag (e.g. a development build on main) we fall
# back to `git describe`, which yields the usual vX.Y.Z-<n>-g<hash> dev version.
set -u

# semver core: vX.Y.Z
core='^v[0-9]+\.[0-9]+\.[0-9]+'

tags=$(git tag --points-at HEAD --sort=-v:refname 2>/dev/null)

pick() {
	printf '%s\n' "$tags" | grep -E "$1" | head -n 1
}

version=""
if [ -n "$tags" ]; then
	for pattern in \
		"${core}\$" \
		"${core}-mocha\$" \
		"${core}-arabica\$" \
		"${core}-rc[0-9]*\$" \
		"${core}-beta\$" \
		"${core}-alpha\$"; do
		version=$(pick "$pattern")
		[ -n "$version" ] && break
	done
fi

# Fall back to git describe for development builds or unrecognized tag formats.
if [ -z "$version" ]; then
	version=$(git describe --tags --always --match "v*" 2>/dev/null)
fi

# Strip the leading v for the embedded version string.
printf '%s\n' "$version" | sed 's/^v//'
