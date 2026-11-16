#!/usr/bin/env bash
# Runs gremlins mutation testing on a curated set of critical packages.
#
# Mutation testing is informational: a low score never fails this script. It
# exits non-zero only when gremlins itself errors (for example, the package
# does not build or its tests fail before any mutation).
#
# Gremlins re-runs a package's tests once per mutant, so the default packages
# are chosen for risk and for a test suite that compiles and runs in seconds.
# Each mutant still relinks the test binary, which costs roughly 10-50s here.
#
# Usage:
#   scripts/test_mutation.sh                 run every default package
#   scripts/test_mutation.sh ./pkg/proof     run the given packages
#   scripts/test_mutation.sh --list-json     print the default packages as JSON
#
# Environment:
#   GREMLINS                      gremlins binary (default: gremlins on PATH)
#   MUTATION_OUT_DIR              directory for JSON reports (default: build/mutation)
#   GREMLINS_WORKERS              parallel mutants (default: 2)
#   GREMLINS_TIMEOUT_COEFFICIENT  per-mutant timeout multiplier (default: 10)
#   GITHUB_STEP_SUMMARY           if set, the score table is appended to it
set -euo pipefail

cd "$(dirname "$0")/.."

# All packages belong to the root module (go.mod at the repository root).
default_packages=(
	./x/blob/types # MsgPayForBlobs and BlobTx validation
	./x/blob/ante  # blob share and gas limits in the ante chain
	./app/ante     # fee, gas and nested-message checks
	./pkg/proof    # share and row inclusion proofs
	./pkg/da       # data availability header and data root
)

if [ "${1:-}" = "--list-json" ]; then
	printf '%s\n' "${default_packages[@]}" | jq -R . | jq -cs .
	exit 0
fi

packages=("$@")
if [ ${#packages[@]} -eq 0 ]; then
	packages=("${default_packages[@]}")
fi

gremlins=${GREMLINS:-gremlins}
out_dir=${MUTATION_OUT_DIR:-build/mutation}
workers=${GREMLINS_WORKERS:-2}
coefficient=${GREMLINS_TIMEOUT_COEFFICIENT:-10}
mkdir -p "$out_dir"

# Gremlins times its coverage run and multiplies it by the coefficient to get
# each mutant's timeout. A cached test result would make that baseline near
# zero and time out every mutant, so force a real run. go vet adds nothing
# per mutant, so skip it.
export GOFLAGS="${GOFLAGS:+$GOFLAGS }-count=1 -vet=off"

summary=${GITHUB_STEP_SUMMARY:-/dev/stdout}
{
	echo "### Mutation testing (gremlins)"
	echo
	echo "Informational only. Efficacy = killed / (killed + lived). Timed-out mutants are excluded."
	echo
	echo "| Package | Killed | Lived | Timed out | Not covered | Efficacy | Mutant coverage |"
	echo "| --- | ---: | ---: | ---: | ---: | ---: | ---: |"
} >>"$summary"

status=0
for pkg in "${packages[@]}"; do
	name=$(echo "$pkg" | sed -E 's#^\./##; s#[/.]+#-#g')
	report="$out_dir/$name.json"
	echo "==> $pkg"
	# Generated protobuf code is excluded: its mutants measure gogoproto, not us.
	if ! "$gremlins" unleash \
		--workers "$workers" \
		--timeout-coefficient "$coefficient" \
		--exclude-files '\.pb(\.gw)?\.go$' \
		--output "$report" \
		"$pkg"; then
		echo "::error::gremlins failed for $pkg"
		echo "| \`$pkg\` | error | | | | | |" >>"$summary"
		status=1
		continue
	fi
	jq -r --arg pkg "$pkg" '
		[.files[]?.mutations[]?.status] as $s
		| def n(x): [$s[] | select(. == x)] | length;
		"| `\($pkg)` | \(n("KILLED")) | \(n("LIVED")) | \(n("TIMED OUT")) | \(n("NOT COVERED"))"
		+ " | \(.test_efficacy // 0 | . * 100 | round / 100)%"
		+ " | \(.mutations_coverage // 0 | . * 100 | round / 100)% |"
	' "$report" >>"$summary"
done

exit $status
