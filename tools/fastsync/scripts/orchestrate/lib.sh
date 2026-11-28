#!/usr/bin/env bash
# lib.sh: helpers shared by the orchestrate/*.sh scripts. Source it; do not run it.
#
# Dry run: with DRY_RUN=1 every command that changes something (gcloud uploads/downloads, the applier,
# celestia-appd, fsboot, tar, rm) is printed as "DRY-RUN: <cmd>" instead of being run. Read-only checks
# that need real inputs are skipped in dry runs.

# Bucket layout (all scripts agree on it):
#   <prefix>/checkpoints/<h>/application.db.tar.zst         app DB after block h (zstd tarball of application.db)
#   <prefix>/checkpoints/<h>/application.db.tar.zst.sha256  sha256sum line of the tarball
#   <prefix>/checkpoints/<h>/CHECKPOINT                     key=value metadata; written LAST = checkpoint complete
#   <prefix>/results/<start>-<end>/RESULT                   key=value segment result (status=PASS|FAIL)
#   <prefix>/results/<start>-<end>/segment.log.tail         last lines of the segment node log
#   <prefix>/progress/<name>.log                            progress logs

DRY_RUN=${DRY_RUN:-0}
# GCLOUD can be overridden, e.g. GCLOUD="env -u CLOUDSDK_AUTH_ACCESS_TOKEN gcloud".
read -r -a GCLOUD_CMD <<< "${GCLOUD:-gcloud}"

log() { echo "[${LOG_TAG:-orchestrate} $(date -u +%H:%M:%S)] $*" >&2; }
die() { echo "[${LOG_TAG:-orchestrate}] ABORT: $*" >&2; exit 1; }

# run CMD...: run a command, or print it in dry-run mode.
run() {
  if [ "$DRY_RUN" = 1 ]; then
    printf 'DRY-RUN:' >&2; printf ' %q' "$@" >&2; echo >&2
    return 0
  fi
  "$@"
}

# run_sh STRING: run a shell pipeline (for pipes/redirects), or print it in dry-run mode.
run_sh() {
  if [ "$DRY_RUN" = 1 ]; then
    echo "DRY-RUN: $1" >&2
    return 0
  fi
  bash -o pipefail -c "$1"
}

is_uint() { [[ $1 =~ ^[0-9]+$ ]]; }
need_uint() { is_uint "$2" || die "$1 must be a non-negative integer, got '$2'"; }

# check_bucket URL: only gs:// prefixes without spaces/quotes/globs (they end up in commands and object names).
check_bucket() {
  [[ $1 =~ ^gs://[a-z0-9][a-z0-9._-]{1,220}(/[A-Za-z0-9._/-]*)?$ ]] || die "bucket must look like gs://bucket[/prefix], got '$1'"
  [[ $1 != *..* ]] || die "bucket prefix must not contain '..'"
}

gcs() { run "${GCLOUD_CMD[@]}" storage "$@"; }

# gcs_exists URL: true if the object exists (always false in dry runs, so dry runs show the full plan).
gcs_exists() {
  [ "$DRY_RUN" = 1 ] && return 1
  "${GCLOUD_CMD[@]}" storage ls "$1" >/dev/null 2>&1
}

# expected_col H COL FILE: column COL of the row for height H in an fshdr expected.tsv
# (h, AppHash(h+1) = app hash after h, hash(header h+1), ...). Empty if absent.
expected_col() { awk -F'\t' -v h="$1" -v c="$2" '$1==h {print $c; exit}' "$3"; }

need_tools() {
  local t
  for t in "$@"; do command -v "$t" >/dev/null 2>&1 || die "missing tool: $t"; done
}

# set_toml FILE SECTION KEY VALUE: set KEY = VALUE (VALUE is written as is, quote strings) in [SECTION] of a
# TOML file, adding the key or the section if missing.
set_toml() {
  local f=$1 sec=$2 key=$3 val=$4 tmp
  tmp=$(mktemp "$f.XXXXXX")
  awk -v sec="[$sec]" -v key="$key" -v val="$val" '
    function emit() { if (insec && !done) { print key " = " val; done = 1 } }
    /^\[/ { emit(); insec = ($0 == sec); print; next }
    insec && $0 ~ "^" key " *=" { if (!done) print key " = " val; done = 1; next }
    { print }
    END { emit(); if (!done) { print ""; print sec; print key " = " val } }' "$f" > "$tmp"
  mv "$tmp" "$f"
}
