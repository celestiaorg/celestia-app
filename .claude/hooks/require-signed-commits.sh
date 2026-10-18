#!/usr/bin/env bash
# PreToolUse hook for Bash: block git commands that override the configured
# commit identity or skip signing. GitHub verifies a signature against the
# committer's account, so committing as someone else with the environment's
# key, or without a signature, produces an unverified commit that the
# repository's signed-commit rule refuses to merge.
set -euo pipefail

cmd=$(jq -r '.tool_input.command // empty')
[[ "$cmd" == *git* ]] || exit 0

patterns=(
  '-c[[:space:]]+user\.(name|email|signingkey)='
  '-c[[:space:]]+(commit|tag)\.gpgsign=false'
  '-c[[:space:]]+gpg\.'
  'GIT_(AUTHOR|COMMITTER)_(NAME|EMAIL)='
  '--author[=[:space:]]'
  '--no-gpg-sign'
  'git[[:space:]]+config[[:space:]]+([^|;&]*[[:space:]])?(user\.(name|email|signingkey)|(commit|tag)\.gpgsign|gpg\.[a-z.]+)[[:space:]]+[^[:space:]-]'
)

for p in "${patterns[@]}"; do
  if grep -Eq -- "$p" <<<"$cmd"; then
    echo "Blocked: this git command overrides the commit identity or skips signing (matched '$p'). Commit with the configured identity and signing key so the commit shows as Verified on GitHub; see 'Signed commits' in AGENTS.md." >&2
    exit 2
  fi
done
exit 0
