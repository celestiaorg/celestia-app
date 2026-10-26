#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
FIXTURE_DIR=$(mktemp -d)
trap 'rm -rf "$FIXTURE_DIR"' EXIT
cd "$SCRIPT_DIR/.."
export RACE_ARGS="$FIXTURE_DIR/args"
cat > "$FIXTURE_DIR/go" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$@" >> "$RACE_ARGS"
MOCK
chmod +x "$FIXTURE_DIR/go"

check_recipe() {
  : > "$RACE_ARGS"
  PATH="$FIXTURE_DIR:$PATH" make --no-print-directory test-race "$@" >/dev/null
  test "$(grep -Fxc 'test' "$RACE_ARGS")" -eq 1
  grep -Fxq -- '-race' "$RACE_ARGS"
  grep -Fxq -- '-count=1' "$RACE_ARGS"
}

check_recipe
grep -Fxq './...' "$RACE_ARGS"
check_recipe PACKAGES=$'example.com/one\nexample.com/two\n'
grep -Fxq 'example.com/one' "$RACE_ARGS"
grep -Fxq 'example.com/two' "$RACE_ARGS"
check_recipe PACKAGES='./fibre/...'
grep -Fxq './fibre/...' "$RACE_ARGS"
echo 'Race recipe argument checks passed'
