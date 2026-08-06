#!/bin/bash
# Usage: ./scripts/commit-tidy-changes.sh
#
# Commits and pushes the result of `make mod` to the current branch. Intended to
# run only from CI, on Dependabot pull requests -- it creates commits and pushes.
#
# test/docker-e2e is a separate Go module that pins the root module by relative
# path (github.com/celestiaorg/celestia-app/v10 => ../..), so its indirect
# requirements are a derived artifact of the root go.mod. A Dependabot bump to a
# root dependency can therefore leave test/docker-e2e untidy and turn
# go-mod-tidy-check red, which a maintainer then has to fix by hand. Regenerating
# and pushing the result keeps those pull requests green. See #7641.

set -euo pipefail

# Use `status --porcelain` rather than `diff --name-only` so that a go.sum being
# created for the first time -- a module with no external deps has none until
# `go mod tidy` gives it one -- is seen as well. --untracked-files=all keeps git
# from collapsing a new directory into a single entry, which would hide whatever
# else is inside it from the check below. Column 4 onwards is the path.
changed=$(git status --porcelain --untracked-files=all | cut -c4-)

if [ -z "${changed}" ]; then
    echo "All modules already tidy; nothing to commit."
    exit 0
fi

# `make mod` must only ever touch go.mod/go.sum. Refuse to commit anything else
# rather than silently pushing unrelated changes to someone's pull request.
unexpected=$(printf '%s\n' "${changed}" | grep -vE '(^|/)go\.(mod|sum)$' || true)
if [ -n "${unexpected}" ]; then
    echo "ERROR: 'make mod' modified files that are not go.mod or go.sum:"
    printf '%s\n' "${unexpected}"
    echo "Refusing to commit. Investigate before re-running."
    exit 1
fi

echo "Committing tidied modules:"
printf '%s\n' "${changed}"

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"

# Safe because every path reported above was validated as a go.mod or go.sum,
# and --all sees exactly that same set (both respect .gitignore).
git add --all
git commit --quiet -m "chore: run make mod to tidy all modules"
git push

echo "Pushed tidied modules."
